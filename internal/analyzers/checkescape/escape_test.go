package checkescape

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
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

func TestListExportRunsInPhysicalDirectory(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go is unavailable: %v", err)
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module lined\n\ngo 1.27\n")
	src := filepath.Join(dir, "lined.go")
	writeFile(t, src, "//line "+filepath.Join(dir, "missing", "elsewhere.go")+":1\npackage lined\n")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{f}, Pkg: types.NewPackage("lined", "lined")}
	archive, err := ListExport(nil)(pass)
	if err != nil {
		t.Fatalf("ListExport followed the //line directive: %v", err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive %q: %v", archive, err)
	}
}

func TestListExportRejectsPackageOutsideFileSet(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "orphan.go", "package orphan\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Fset: token.NewFileSet(), Files: []*ast.File{f}}
	if _, err := ListExport(nil)(pass); err == nil {
		t.Fatal("ListExport returned no error for a package clause outside the file set")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
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
