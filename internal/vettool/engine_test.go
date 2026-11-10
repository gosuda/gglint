package vettool

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

func newPass(t *testing.T, name, src string, report func(analysis.Diagnostic)) *analysis.Pass {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return &analysis.Pass{
		Fset:   fset,
		Files:  []*ast.File{file},
		Report: report,
		ReadFile: func(filename string) ([]byte, error) {
			if filename != name {
				return nil, fs.ErrNotExist
			}
			return []byte(src), nil
		},
	}
}

func newTestEngine(t *testing.T, cfg config.Config) *engine {
	t.Helper()
	e, _, err := newEngine(cfg)
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	return e
}

func probeRule() rules.Rule {
	probe := &analysis.Analyzer{
		Name: "probe",
		Doc:  "reports the first declaration of the first file",
		Run: func(pass *analysis.Pass) (any, error) {
			pass.Reportf(pass.Files[0].Decls[0].Pos(), "probe finding")
			return nil, nil
		},
	}
	return rules.Rule{Name: "probe", Build: func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{probe} }}
}

func TestFilterUsesRealFileNameNotLineDirective(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  config.Config
		file string
		src  string
		want int
	}{
		{
			name: "test file renamed to a non-test file is dropped when tests are off",
			cfg:  config.Config{Mode: config.Standard},
			file: "/work/p/x_test.go",
			src:  "package p\n\n//line x.go:1\nvar X = 1\n",
			want: 0,
		},
		{
			name: "non-test file renamed to a test file is kept when tests are off",
			cfg:  config.Config{Mode: config.Standard},
			file: "/work/p/x.go",
			src:  "package p\n\n//line x_test.go:1\nvar X = 1\n",
			want: 1,
		},
		{
			name: "exclude path matches the real path relative to the exclude base",
			cfg: config.Config{
				Mode:        config.Standard,
				Tests:       true,
				ExcludeBase: "/work",
				Excludes:    []config.Exclude{{Paths: []string{`^p/real\.go$`}}},
			},
			file: "/work/p/real.go",
			src:  "package p\n\n//line /work/p/other.go:1\nvar X = 1\n",
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, tc.cfg)
			var got []analysis.Diagnostic
			pass := newPass(t, tc.file, tc.src, func(d analysis.Diagnostic) { got = append(got, d) })
			for _, a := range e.analyzers([]rules.Rule{probeRule()}) {
				if _, err := a.Run(pass); err != nil {
					t.Fatalf("run %s: %v", a.Name, err)
				}
			}
			if len(got) != tc.want {
				t.Fatalf("kept %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func loadTinyPackage(t *testing.T) *packages.Package {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.27\n",
		"p.go":   "package p\n\nimport \"sync\"\n\ntype T struct {\n\tmu sync.Mutex\n\tn int\n}\n\nfunc (t *T) A() { t.mu.Lock(); t.n++; t.mu.Unlock() }\n\nfunc (t *T) B() { t.mu.Lock(); t.n--; t.mu.Unlock() }\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cfg := &packages.Config{
		Context: t.Context(),
		Dir:     dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
			packages.NeedTypesSizes | packages.NeedModule,
		Env: append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("load %s: want one clean package, got %v", dir, pkgs)
	}
	return pkgs[0]
}

func analyzeWithEngine(cfg config.Config, pkg *packages.Package) error {
	e, selected, err := newEngine(cfg)
	if err != nil {
		return fmt.Errorf("mode %v: newEngine: %w", cfg.Mode, err)
	}
	graph, err := checker.Analyze(e.reporting(selected), []*packages.Package{pkg}, nil)
	if err != nil {
		return fmt.Errorf("mode %v: analyze: %w", cfg.Mode, err)
	}
	var failures []error
	found := 0
	for _, root := range graph.Roots {
		if root.Err != nil {
			failures = append(failures, fmt.Errorf("mode %v: %s: %w", cfg.Mode, root.Analyzer.Name, root.Err))
		}
		if root.Analyzer.Name == "checklocks" {
			for _, d := range root.Diagnostics {
				if strings.Contains(d.Message, "may require checklocks annotation") {
					found++
				}
			}
		}
	}
	if want := cfg.Mode >= config.Strict; (found > 0) != want {
		failures = append(failures, fmt.Errorf("mode %v: checklocks reported %d findings, want any = %v", cfg.Mode, found, want))
	}
	return errors.Join(failures...)
}

func TestConcurrentBuildsKeepSharedAnalyzersIsolated(t *testing.T) {
	t.Parallel()
	const workersPerMode = 3
	type job struct {
		cfg config.Config
		pkg *packages.Package
	}
	var jobs []job
	for _, mode := range []config.Mode{config.Basic, config.Standard, config.Strict} {
		for range workersPerMode {
			cfg := config.Config{Mode: mode, Complexity: config.ComplexityFor(mode), Tests: true}
			if mode < config.Strict {
				cfg.Enable = []string{"checklocks"}
			}
			jobs = append(jobs, job{cfg: cfg, pkg: loadTinyPackage(t)})
		}
	}
	errs := make(chan error, len(jobs))
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Go(func() { errs <- analyzeWithEngine(job.cfg, job.pkg) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestReportOutsideTestsSkipsTestFiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
		want int
	}{
		{name: "non-test file kept", file: "/work/p/a.go", want: 1},
		{name: "test file dropped", file: "/work/p/a_test.go", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEngine(t, config.Config{Mode: config.Standard, Tests: true})
			var got []analysis.Diagnostic
			pass := newPass(t, tc.file, "package p\n\nvar X = 1\n", func(d analysis.Diagnostic) { got = append(got, d) })
			r := probeRule()
			r.Reports = rules.ReportOutsideTests
			for _, a := range e.analyzers([]rules.Rule{r}) {
				if _, err := a.Run(pass); err != nil {
					t.Fatalf("run %s: %v", a.Name, err)
				}
			}
			if len(got) != tc.want {
				t.Fatalf("kept %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

func TestReportWithFixKeepsOnlyFindingsWithEdits(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t, config.Config{Mode: config.Standard})
	pass := newPass(t, "/work/p/a.go", "package p\n\nvar X = 1\n", nil)
	pos := pass.Files[0].Decls[0].Pos()
	r := probeRule()
	r.Reports = rules.ReportWithFix
	cases := []struct {
		name string
		d    analysis.Diagnostic
		want bool
	}{
		{
			name: "no fix dropped",
			d:    analysis.Diagnostic{Pos: pos, Message: "probe finding"},
			want: false,
		},
		{
			name: "fix without edits dropped",
			d:    analysis.Diagnostic{Pos: pos, Message: "probe finding", SuggestedFixes: []analysis.SuggestedFix{{Message: "fix it"}}},
			want: false,
		},
		{
			name: "fix with edits kept",
			d:    analysis.Diagnostic{Pos: pos, Message: "probe finding", SuggestedFixes: []analysis.SuggestedFix{{Message: "fix it", TextEdits: []analysis.TextEdit{{Pos: pos, End: pos, NewText: []byte("var Y = 1")}}}}},
			want: true,
		},
	}
	for _, tc := range cases {
		if got := e.keep(pass, r, "", tc.d); got != tc.want {
			t.Errorf("%s: keep = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestInvalidPositionDroppedForEveryFilter(t *testing.T) {
	t.Parallel()
	for _, reports := range []rules.Reports{rules.ReportEverywhere, rules.ReportOutsideTests, rules.ReportWithFix} {
		e := newTestEngine(t, config.Config{Mode: config.Standard})
		pass := newPass(t, "/work/p/a.go", "package p\n\nvar X = 1\n", nil)
		pos := pass.Files[0].Decls[0].Pos()
		r := probeRule()
		r.Reports = reports
		d := analysis.Diagnostic{Pos: token.NoPos, Message: "probe finding"}
		if reports == rules.ReportWithFix {
			d.SuggestedFixes = []analysis.SuggestedFix{{Message: "fix it", TextEdits: []analysis.TextEdit{{Pos: pos, End: pos, NewText: []byte("var Y = 1")}}}}
		}
		if e.keep(pass, r, "", d) {
			t.Errorf("reports %v: keep with no position = true, want false", reports)
		}
	}
}

func TestFilteredFindingLeavesNolintDirectiveUnused(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t, config.Config{Mode: config.Standard, Tests: true})
	e.known["probe"], e.selected["probe"] = true, true
	src := "package p\n\nvar X = 1 //nolint:probe // reason\n"
	var kept []analysis.Diagnostic
	pass := newPass(t, "/work/p/a.go", src, func(d analysis.Diagnostic) { kept = append(kept, d) })
	r := probeRule()
	r.Reports = rules.ReportWithFix
	for _, a := range e.analyzers([]rules.Rule{r}) {
		if _, err := a.Run(pass); err != nil {
			t.Fatalf("run %s: %v", a.Name, err)
		}
	}
	if len(kept) != 0 {
		t.Fatalf("kept %d findings, want 0", len(kept))
	}
	var messages []string
	nolintPass := newPass(t, "/work/p/a.go", src, func(d analysis.Diagnostic) { messages = append(messages, d.Message) })
	if _, err := e.nolintAnalyzer(nil).Run(nolintPass); err != nil {
		t.Fatalf("run nolint: %v", err)
	}
	if len(messages) != 1 || messages[0] != "unused nolint directive for probe" {
		t.Fatalf("nolint reported %q, want %q", messages, []string{"unused nolint directive for probe"})
	}
}
