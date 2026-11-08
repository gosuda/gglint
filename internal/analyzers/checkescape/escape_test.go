package checkescape

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzertest"
)

func TestEscapeModule(t *testing.T) {
	requireObjdump(t)
	analyzertest.RunIgnoringFacts(t, expandEscapeModule(t), New(ListExport(nil)), "./...")
}

func TestBinaryArchiveUsesBuildTags(t *testing.T) {
	requireObjdump(t)
	tags := []string{"checkescape_binary"}
	pkgs, err := packages.Load(&packages.Config{
		Mode:       packages.LoadAllSyntax,
		Dir:        "testdata/escape3",
		BuildFlags: []string{"-tags=" + strings.Join(tags, ",")},
		Env:        append(os.Environ(), "GO111MODULE=on", "GOPROXY=off", "GOWORK=off"),
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("testdata/escape3 does not load")
	}
	graph, err := checker.Analyze([]*analysis.Analyzer{New(ListExport(tags))}, pkgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, act := range graph.Roots {
		if act.Err != nil {
			t.Fatalf("%s: %v", act, act.Err)
		}
		for _, d := range act.Diagnostics {
			t.Errorf("%s: unexpected diagnostic: %s", act.Package.Fset.Position(d.Pos), d.Message)
		}
		requireHeapEscape(t, act, "unusedAllocation")
	}
}

func requireHeapEscape(t *testing.T, act *checker.Action, name string) {
	t.Helper()
	obj := act.Package.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s is not declared: the build tags were not applied", name)
	}
	var escapes Escapes
	if !act.ObjectFact(obj, &escapes) {
		t.Fatalf("%s has no Escapes fact", name)
	}
	if escapes.CallSites[allocation] == nil {
		t.Errorf("%s escapes = %+v, want a heap allocation", name, escapes)
	}
}

func requireObjdump(t *testing.T) {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go tool objdump is unavailable: %v", err)
	}
	if out, err := exec.CommandContext(t.Context(), goTool, "tool", "-n", "objdump").CombinedOutput(); err != nil {
		t.Skipf("go tool objdump is unavailable: %v: %s", err, out)
	}
}

func expandEscapeModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/escape")); err != nil {
		t.Fatal(err)
	}
	test1 := filepath.Join(dir, "test1")
	template, err := os.ReadFile(filepath.Join(test1, "nosplit.go.in"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.NewReplacer(
		"{CR}", "\r",
		"{NBSP}", "\u00a0",
		"{SPACE}", " ",
		"{TAB}", "\t",
	).Replace(string(template))
	if err := os.WriteFile(filepath.Join(test1, "nosplit.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
