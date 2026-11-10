package wgstate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/wgstate"
)

func fuzzOps(data []byte) []int {
	n := len(data)
	if n > 8 {
		n = 8
	}
	ops := make([]int, 0, n)
	for _, b := range data[:n] {
		ops = append(ops, int(b)%6)
	}
	return ops
}

func fuzzStmt(op int) string {
	switch op {
	case 0:
		return "wg.Add(1)"
	case 1:
		return "wg.Done()"
	case 2:
		return "go func() { defer wg.Done() }()"
	case 3:
		return "g.Go(func() error { return nil })"
	case 4:
		return "wg.Wait()"
	default:
		return "_ = g.Wait()"
	}
}

func fuzzOrder(ops []int) []int {
	var out []int
	for _, want := range []int{0, 1, 2, 3, 4, 5} {
		for _, op := range ops {
			if op == want {
				out = append(out, op)
			}
		}
	}
	return out
}

func fuzzSrc(data []byte) string {
	var b strings.Builder
	b.WriteString("package p\n\nimport (\n\"sync\"\n\n\"golang.org/x/sync/errgroup\"\n)\n\nfunc F(wg sync.WaitGroup, g *errgroup.Group) {\n")
	for _, op := range fuzzOrder(fuzzOps(data)) {
		b.WriteString(fuzzStmt(op))
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func fuzzFiles(src string) map[string]string {
	return map[string]string{
		"go.mod":               "module golang.org/x/sync\n\ngo 1.27\n",
		"errgroup/errgroup.go": "package errgroup\n\ntype Group struct {\nerr error\n}\n\nfunc (g *Group) Go(f func() error) {\ng.err = f()\n}\n\nfunc (g *Group) TryGo(f func() error) bool {\ng.err = f()\nreturn true\n}\n\nfunc (g *Group) Wait() error {\nreturn g.err\n}\n",
		"p/p.go":               src,
	}
}

func fuzzWrite(t *testing.T, dir, src string) {
	t.Helper()
	for name, content := range fuzzFiles(src) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func fuzzAnalyze(t *testing.T, pkgs []*packages.Package) *checker.Graph {
	t.Helper()
	var graph *checker.Graph
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("analyze panic: %v", r)
			}
		}()
		graph, err = checker.Analyze(wgstate.Analyzers(), pkgs, nil)
	}()
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	return graph
}

func fuzzCheck(t *testing.T, dir string) {
	t.Helper()
	cfg := &packages.Config{
		Context: t.Context(),
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule,
		Env:     append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, "./p")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("load: want one clean package, got %v", pkgs)
	}
	for _, root := range fuzzAnalyze(t, pkgs).Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
		if len(root.Diagnostics) > 0 {
			t.Fatalf("%s: got %d diagnostics, want zero", root.Analyzer.Name, len(root.Diagnostics))
		}
	}
}

func FuzzNoPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{2})
	f.Add([]byte{0, 2, 4})
	f.Add([]byte{0, 1, 4})
	f.Add([]byte{3, 5})
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{5, 4, 3, 2, 1, 0, 0, 2})
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		fuzzWrite(t, dir, fuzzSrc(data))
		fuzzCheck(t, dir)
	})
}
