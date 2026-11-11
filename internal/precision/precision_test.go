package precision_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var corpus = []string{
	"./archive/tar",
	"./bufio",
	"./bytes",
	"./compress/gzip",
	"./context",
	"./database/sql",
	"./encoding/csv",
	"./encoding/json",
	"./errors",
	"./flag",
	"./fmt",
	"./go/parser",
	"./io",
	"./io/fs",
	"./log/slog",
	"./mime/multipart",
	"./net/http",
	"./net/url",
	"./os",
	"./path/filepath",
	"./strings",
	"./sync",
	"./text/template",
}

type finding struct {
	Rule    string `json:"rule"`
	Check   string `json:"check"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type document struct {
	Findings []finding `json:"findings"`
}

type overlap struct {
	A       string
	B       string
	Shared  []string
	Smaller int
}

func tokenOf(f finding) string {
	if f.Check == "" {
		return f.Rule
	}
	return f.Rule + ":" + f.Check
}

func lineKey(f finding) string {
	return f.Path + ":" + strconv.Itoa(f.Line)
}

func isTestPath(p string) bool {
	return strings.HasSuffix(p, "_test.go")
}

func isNewRule(rule string) bool {
	switch rule {
	case "errdrop",
		"chanstate",
		"lockstate",
		"wgstate",
		"gorace",
		"ctxflow",
		"resstate",
		"alias",
		"spin":
		return true
	default:
		return false
	}
}

func newRuleBudget(rule string) int {
	if rule == "errdrop" {
		return 6
	}
	return 2
}

func newRuleViolations(fs []finding) map[string][]finding {
	grouped := make(map[string][]finding)
	for _, f := range fs {
		if isTestPath(f.Path) {
			continue
		}
		if !isNewRule(f.Rule) {
			continue
		}
		grouped[f.Rule] = append(grouped[f.Rule], f)
	}
	over := make(map[string][]finding)
	for rule, list := range grouped {
		if len(list) > newRuleBudget(rule) {
			over[rule] = list
		}
	}
	return over
}

func isExcludedRule(rule string) bool {
	switch rule {
	case "modernize",
		"gofumpt",
		"gosimple",
		"misspell",
		"usetesting",
		"complexity":
		return true
	default:
		return false
	}
}

func excessOtherFindings(fs []finding) []finding {
	kept := make([]finding, 0, len(fs))
	for _, f := range fs {
		if isExcludedRule(f.Rule) {
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) <= 400 {
		return nil
	}
	return kept
}

func intersect(a, b map[string]struct{}) []string {
	var shared []string
	for k := range a {
		if _, ok := b[k]; ok {
			shared = append(shared, k)
		}
	}
	slices.Sort(shared)
	return shared
}

func overlappingPairs(fs []finding) []overlap {
	byToken := make(map[string]map[string]struct{})
	for _, f := range fs {
		tok := tokenOf(f)
		set := byToken[tok]
		if set == nil {
			set = make(map[string]struct{})
			byToken[tok] = set
		}
		set[lineKey(f)] = struct{}{}
	}
	tokens := slices.Sorted(maps.Keys(byToken))
	var out []overlap
	for i := range tokens {
		for j := i + 1; j < len(tokens); j++ {
			a := tokens[i]
			b := tokens[j]
			shared := intersect(byToken[a], byToken[b])
			if len(shared) < 4 {
				continue
			}
			smaller := min(len(byToken[a]), len(byToken[b]))
			if len(shared)*4 < smaller {
				continue
			}
			out = append(out, overlap{A: a, B: b, Shared: shared, Smaller: smaller})
		}
	}
	return out
}

func formatFindings(fs []finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(lineKey(f))
		b.WriteString(" ")
		b.WriteString(tokenOf(f))
		b.WriteString(" ")
		b.WriteString(f.Message)
		b.WriteString("\n")
	}
	return b.String()
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func buildGglint(t *testing.T, root, bin string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "./cmd/gglint")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GO111MODULE=on", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
}

func writeStandardConfig(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("mode = \"standard\"\n"), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
}

func gorootSrc(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT failed: %v", err)
	}
	goroot := strings.TrimSpace(string(out))
	if goroot == "" {
		t.Fatal("go env GOROOT printed empty output")
	}
	return filepath.Join(goroot, "src")
}

func goBuildCache(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOCACHE failed: %v", err)
	}
	cache := strings.TrimSpace(string(out))
	if cache == "" {
		t.Fatal("go env GOCACHE printed empty output")
	}
	return cache
}

func runCorpus(t *testing.T, bin, cfg, src string) []finding {
	t.Helper()
	args := make([]string, 0, len(corpus)+5)
	args = append(args, "run", "-config", cfg, "-format", "json")
	args = append(args, corpus...)
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "GO111MODULE=on", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOWORK=off", "XDG_CACHE_HOME="+filepath.Join(filepath.Dir(cfg), "cache"), "GOCACHE="+goBuildCache(t))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](runErr)
		if !ok || exitErr.ExitCode() != 1 {
			t.Fatalf("gglint run failed: %v\nstdout:\n%s\nstderr:\n%s", runErr, stdout.String(), stderr.String())
		}
	}
	var doc document
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("gglint run failed: %v: output is not JSON: %v\nstdout:\n%s\nstderr:\n%s", runErr, err, stdout.String(), stderr.String())
	}
	return doc.Findings
}

func reportNewRules(t *testing.T, over map[string][]finding) {
	t.Helper()
	rules := slices.Sorted(maps.Keys(over))
	for _, rule := range rules {
		list := over[rule]
		t.Errorf("rule %q reports %d non-test findings (budget %d):\n%s", rule, len(list), newRuleBudget(rule), formatFindings(list))
	}
}

func reportPairs(t *testing.T, pairs []overlap) {
	t.Helper()
	for _, p := range pairs {
		t.Errorf("tokens %q and %q share %d lines over %d findings of the smaller one:\n%s", p.A, p.B, len(p.Shared), p.Smaller, strings.Join(p.Shared, "\n"))
	}
}

func TestCorpusBudget(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping corpus precision gate in short mode")
	}
	tmp := t.TempDir()
	root := moduleRoot(t)
	bin := filepath.Join(tmp, "gglint")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	buildGglint(t, root, bin)
	cfg := filepath.Join(tmp, "gglint.toml")
	writeStandardConfig(t, cfg)
	src := gorootSrc(t)
	findings := runCorpus(t, bin, cfg, src)
	if over := newRuleViolations(findings); len(over) != 0 {
		reportNewRules(t, over)
	}
	if other := excessOtherFindings(findings); other != nil {
		t.Errorf("other rules report %d findings (budget 400):\n%s", len(other), formatFindings(other))
	}
	if pairs := overlappingPairs(findings); len(pairs) != 0 {
		reportPairs(t, pairs)
	}
}
