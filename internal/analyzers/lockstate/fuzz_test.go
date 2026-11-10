package lockstate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/lockstate"
)

func decodeFuzzOps(data []byte) []string {
	ops := []string{"Lock", "Unlock", "RLock", "RUnlock"}
	n := min(len(data), 8)
	out := make([]string, 0, n)
	for _, b := range data[:n] {
		out = append(out, ops[int(b)%len(ops)])
	}
	return out
}

func renderFuzzSrc(ops []string) string {
	lines := []string{"package p", "", `import "sync"`, "", "func sink(v ...any) {}", "", "func f(mu *sync.RWMutex) {"}
	for _, op := range ops {
		lines = append(lines, "sink(mu)", "mu."+op+"()")
	}
	lines = append(lines, "}", "")
	return strings.Join(lines, "\n")
}

func writeFuzzMod(t *testing.T, dir, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fuzzp\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadFuzzPkg(t *testing.T, dir string) *packages.Package {
	t.Helper()
	cfg := &packages.Config{
		Dir: dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
			packages.NeedTypesSizes | packages.NeedModule,
		Env: append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("load: %v", pkgs[0].Errors)
	}
	return pkgs[0]
}

func assertFuzzClean(t *testing.T, pkg *packages.Package) {
	t.Helper()
	graph, err := checker.Analyze(lockstate.Analyzers(), []*packages.Package{pkg}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
		if len(root.Diagnostics) > 0 {
			t.Fatalf("%s: %d diagnostics, first: %v", root.Analyzer.Name, len(root.Diagnostics), root.Diagnostics[0].Message)
		}
	}
}

func checkFuzzSrc(t *testing.T, src string) {
	t.Helper()
	dir := t.TempDir()
	writeFuzzMod(t, dir, src)
	assertFuzzClean(t, loadFuzzPkg(t, dir))
}

func FuzzNoPanic(f *testing.F) {
	f.Add([]byte{1})
	f.Add([]byte{0, 1})
	f.Add([]byte{2, 3})
	f.Add([]byte{2, 1})
	f.Add([]byte{2, 0})
	f.Add([]byte{0})
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{3, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		checkFuzzSrc(t, renderFuzzSrc(decodeFuzzOps(data)))
	})
}
