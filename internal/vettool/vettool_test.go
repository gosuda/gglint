package vettool

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/nilness"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func buildUnit(t *testing.T, cfg config.Config, unit string) []*analysis.Analyzer {
	t.Helper()
	configPath := writeFile(t, "config.json", cfg.Encode())
	unitPath := writeFile(t, "unit.cfg", []byte(unit))
	got, err := build(configPath, unitPath)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return got
}

func nameCounts(analyzers []*analysis.Analyzer) map[string]int {
	counts := map[string]int{}
	for _, a := range analyzers {
		counts[a.Name]++
	}
	return counts
}

func TestBuildFactsOnlyUnitReturnsOnlyFactProducers(t *testing.T) {
	got := buildUnit(t, config.Config{Mode: config.Strict}, `{"VetxOnly":true}`)
	for _, a := range got {
		if len(a.FactTypes) == 0 {
			t.Errorf("facts-only unit runs %s, which produces no facts", a.Name)
		}
	}
	counts := nameCounts(got)
	for name, n := range counts {
		if n > 1 {
			t.Errorf("facts-only unit has %d analyzers named %s", n, name)
		}
	}
	for _, absent := range []string{rules.NolintName, "gosec", "unparam"} {
		if counts[absent] > 0 {
			t.Errorf("facts-only unit runs non-producer %s", absent)
		}
	}
	if slices.Contains(got, nilness.Analyzer) {
		t.Errorf("facts-only unit runs the go/analysis nilness pass, which produces no facts")
	}
	for _, present := range []string{"printf", "checklocks"} {
		if counts[present] != 1 {
			t.Errorf("facts-only unit runs fact producer %s %d times, want 1", present, counts[present])
		}
	}
	if err := analysis.Validate(got); err != nil {
		t.Fatalf("facts-only analyzers fail validation: %v", err)
	}
}

func TestBuildReportingUnitAddsNolint(t *testing.T) {
	got := buildUnit(t, config.Config{Mode: config.Strict}, `{"VetxOnly":false}`)
	counts := nameCounts(got)
	for _, present := range []string{rules.NolintName, "gosec", "unparam", "printf"} {
		if counts[present] != 1 {
			t.Errorf("reporting unit runs %s %d times, want 1", present, counts[present])
		}
	}
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	t.Cleanup(func() { os.Stderr = saved })
	os.Stderr = w
	run()
	os.Stderr = saved
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("close pipe writer: %v", closeErr)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}
	return string(out)
}

func TestMainReportsMissingConfigAsInternalError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	unit := writeFile(t, "unit.cfg", []byte(`{"VetxOnly":false}`))
	code := 0
	stderr := captureStderr(t, func() { code = Main([]string{"-config=" + missing, unit}) })
	if code != 3 {
		t.Fatalf("Main exit code = %d, want 3", code)
	}
	if !strings.HasPrefix(stderr, "gglint: internal: ") || !strings.Contains(stderr, missing) {
		t.Fatalf("stderr = %q, want a gglint: internal: line naming %s", stderr, missing)
	}
}

func panicking() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:      "boom",
		Doc:       "panics on every package",
		FactTypes: []analysis.Fact{new(boomFact)},
		Run:       func(*analysis.Pass) (any, error) { panic("boom") },
	}
}

type boomFact struct{}

func (*boomFact) AFact() {}

func TestReportPanicsTurnsPanicIntoError(t *testing.T) {
	wrapped := reportPanics([]*analysis.Analyzer{panicking()})
	_, err := wrapped[0].Run(&analysis.Pass{})
	if err == nil {
		t.Fatalf("reporting unit returned no error for a panicking analyzer")
	}
	if !strings.Contains(err.Error(), "panic: boom") {
		t.Fatalf("error = %q, want it to contain %q", err, "panic: boom")
	}
}

func runPanics(a *analysis.Analyzer) (recovered any) {
	defer func() { recovered = recover() }()
	_, err := a.Run(&analysis.Pass{})
	return err
}

func TestFactsOnlyUnitDoesNotRecoverPanics(t *testing.T) {
	e := newTestEngine(t, config.Config{Mode: config.Standard})
	boom := panicking()
	rule := rules.Rule{Name: "boom", Build: func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{boom} }}
	got := e.factsOnly([]rules.Rule{rule})
	if len(got) != 1 || got[0] != boom {
		t.Fatalf("facts-only unit = %v, want only the boom producer", got)
	}
	if recovered := runPanics(got[0]); recovered != "boom" {
		t.Fatalf("facts-only Run recovered the panic (got %v), want it to propagate", recovered)
	}
}
