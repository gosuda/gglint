package vettool

import (
	json "encoding/json/v2"
	"flag"
	"fmt"
	"os"
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

func Main(args []string) int {
	flag.String(configFlag, "", "path of the resolved gglint configuration")
	flag.String(fixOutputFlag, "", "directory for staged automatic fixes")
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
		unit, err := stageUnit(args[len(args)-1], fixOutput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gglint: internal: %v\n", err)
			return 3
		}
		if unit != "" {
			revised := []string{os.Args[0]}
			for _, arg := range args[:len(args)-1] {
				name := strings.TrimLeft(arg, "-")
				if name != "json" && !strings.HasPrefix(name, "json=") {
					revised = append(revised, arg)
				}
			}
			os.Args = append(revised, "-fix", unit)
		}
	}
	unitchecker.Main(analyzers...)
	return 0
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

func stageUnit(path, directory string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read fix unit: %w", err)
	}
	var unit unitchecker.Config
	if err := json.Unmarshal(data, &unit); err != nil {
		return "", fmt.Errorf("decode fix unit: %w", err)
	}
	if unit.VetxOnly {
		return "", nil
	}
	file, err := os.CreateTemp(directory, "unit-*.cfg")
	if err != nil {
		return "", fmt.Errorf("create fix unit: %w", err)
	}
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
