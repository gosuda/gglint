package vettool

import (
	json "encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

const (
	configFlag    = "config"
	fixOutputFlag = "fix-output"
)

// ActiveRootsFlag carries the invoking gglint process's resolved main
// modules to the vettool. It is declared in Main so the go command forwards
// it: go vet rejects any flag the tool does not name in its flag set.
const ActiveRootsFlag = "active-roots"

func Main(args []string) int {
	// Declared so the go command forwards the flag to the tool: go vet
	// queries the tool's flag set and rejects anything it does not name.
	flag.String(configFlag, "", "path of the resolved gglint configuration")
	flag.String(fixOutputFlag, "", "directory for staged automatic fixes")
	flag.String(ActiveRootsFlag, "", "invocation's resolved main modules as a JSON array")
	path, found := configPath(args)
	if !found {
		unitchecker.Main()
		return 0
	}
	fixOutput, _ := optionPath(args, fixOutputFlag)
	analyzers, err := build(path, args[len(args)-1], wantFix(args) || fixOutput != "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gglint: internal: %v\n", err)
		return 3
	}
	if fixOutput != "" {
		if err := stageFix(args, fixOutput); err != nil {
			fmt.Fprintf(os.Stderr, "gglint: internal: %v\n", err)
			return 3
		}
	}
	unitchecker.Main(analyzers...)
	return 0
}

// stageFix stages the unit configuration for a fix run and rewrites the
// process arguments to run the fix through the staged unit.
func stageFix(args []string, fixOutput string) error {
	unit, err := stageUnit(args[len(args)-1], fixOutput, parseActiveRoots(args))
	if err != nil {
		return err
	}
	if unit == "" {
		return nil
	}
	revised := []string{os.Args[0]}
	for _, arg := range args[:len(args)-1] {
		name := strings.TrimLeft(arg, "-")
		if name != "json" && !strings.HasPrefix(name, "json=") {
			revised = append(revised, arg)
		}
	}
	revised = append(revised, "-fix", unit)
	os.Args = revised
	return nil
}

func configPath(args []string) (string, bool) {
	return optionPath(args, configFlag)
}

func optionPath(args []string, option string) (string, bool) {
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if value, ok := strings.CutPrefix(name, option+"="); ok {
			return value, true
		}
		if name == option && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func stageUnit(path, directory string, roots []string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read fix unit: %w", err)
	}
	var unit unitchecker.Config
	if err = json.Unmarshal(data, &unit); err != nil {
		return "", fmt.Errorf("decode fix unit: %w", err)
	}
	if unit.VetxOnly {
		return "", nil
	}

	if !stageEligible(&unit, roots) {
		return "", nil
	}
	tmpFile, createErr := os.CreateTemp(directory, "unit-*.cfg")
	if createErr != nil {
		return "", fmt.Errorf("create fix unit: %w", createErr)
	}
	file := tmpFile
	unit.FixArchive = file.Name() + ".fix.zip"
	data, err = json.Marshal(&unit)
	if err == nil {
		_, err = file.Write(data)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("write fix unit: %w", err)
	}
	return file.Name(), nil
}

// stageEligible reports whether fixes for the unit may be staged: main
// modules always, metadata-less units only when verified in-module.
func stageEligible(unit *unitchecker.Config, roots []string) bool {
	if unit.Module != nil {
		return unit.Module.Main
	}
	// Synthetic file packages carry no module metadata: verify their
	// files against the enclosing module instead of skipping their
	// fixes silently.
	return verifiedModuleUnit(unit, roots)
}

// verifiedModuleUnit reports whether every file in the unit sits inside one
// active module and outside its vendor directories. Synthetic file packages
// carry no module metadata, so their scope is verified by directory.
// A nil roots slice selects workspace discovery from the working directory;
// the fix invocation passes its resolved roots so both ends stage the same
// files even when the vettool's package directory sits under a different
// go.work.
func verifiedModuleUnit(unit *unitchecker.Config, roots []string) bool {
	if len(unit.GoFiles) == 0 {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	if roots == nil {
		roots = config.ActiveModuleRoots(cwd)
	}
	if len(roots) == 0 {
		return false
	}
	root, ok := unitModuleRoot(unit.GoFiles, cwd, roots)
	if !ok {
		return false
	}
	// The standard library ships its own go.mod (module std) but is
	// never the main module: never stage fixes for it.
	if path, ok := modulePathOf(root); !ok || path == "std" {
		return false
	}
	return true
}

// parseActiveRoots decodes the invocation's resolved main modules from the
// command line. An absent flag means the vettool runs outside a fix
// invocation, and the caller falls back to workspace discovery. A present
// but undecodable flag fails closed to no roots.
func parseActiveRoots(args []string) []string {
	value, found := optionPath(args, ActiveRootsFlag)
	if !found {
		return nil
	}
	var roots []string
	if err := json.Unmarshal([]byte(value), &roots); err != nil {
		return []string{}
	}
	return roots
}

// unitModuleRoot returns the single enclosing module of every file, or
// reports false when a file sits outside any active module, files span
// modules, or a file sits under a vendor directory.
func unitModuleRoot(files []string, cwd string, roots []string) (string, bool) {
	root := ""
	for _, name := range files {
		abs := name
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, name)
		}
		dir := filepath.Dir(abs)
		found, ok := config.EnclosingModule(dir)
		if !ok || !config.ContainsRoot(roots, found) {
			return "", false
		}
		if root == "" {
			root = found
		} else if root != found {
			return "", false
		}
		if config.InModuleVendor(root, dir) {
			return "", false
		}
	}
	return root, root != ""
}

// modulePathOf reports the module path declared by the go.mod in root.
func modulePathOf(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1], true
		}
	}
	return "", false
}

func vetxOnly(unitConfig string) (bool, error) {
	data, err := os.ReadFile(unitConfig)
	if err != nil {
		return false, err
	}
	var unit struct{ VetxOnly bool }
	if err := json.Unmarshal(data, &unit); err != nil {
		return false, fmt.Errorf("decode unit config %s: %w", unitConfig, err)
	}
	return unit.VetxOnly, nil
}

func wantFix(args []string) bool {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if name == "fix" || strings.HasPrefix(name, "fix=") {
			return true
		}
	}
	return false
}

func build(path, unitConfig string, fix bool) ([]*analysis.Analyzer, error) {
	factsOnly, err := vetxOnly(unitConfig)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Decode(data)
	if err != nil {
		return nil, err
	}
	e, selected, err := newEngine(cfg)
	if err != nil {
		return nil, err
	}
	e.fix = fix
	if fix {
		// Single-threaded startup owns this read: unitchecker runs the
		// analyzers, and their refusal lines, only after build returns.
		e.err = os.Stderr
	}
	if factsOnly {
		return e.factsOnly(selected), nil
	}
	return e.reporting(selected), nil
}

func (e *engine) factsOnly(selected []rules.Rule) []*analysis.Analyzer {
	seen := map[*analysis.Analyzer]bool{}
	var out []*analysis.Analyzer
	var visit func(*analysis.Analyzer)
	visit = func(a *analysis.Analyzer) {
		if seen[a] {
			return
		}
		seen[a] = true
		if len(a.FactTypes) > 0 {
			out = append(out, a)
		}
		for _, req := range a.Requires {
			visit(req)
		}
	}
	for _, a := range e.analyzers(selected) {
		visit(a)
	}
	return out
}

func (e *engine) reporting(selected []rules.Rule) []*analysis.Analyzer {
	built := reportPanics(e.analyzers(selected))
	if e.selected[rules.NolintName] {
		built = append(built, e.nolintAnalyzer(built))
	}
	return built
}
