package resstate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/resstate"
)

func resDecode(data []byte) []int {
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

func resStmt(op int) string {
	switch op {
	case 0:
		return "r.Close()"
	case 1:
		return "r.Use()"
	case 2:
		return "sink(r)"
	case 3:
		return "p.Put(r)"
	case 4:
		return "_ = r.n"
	default:
		return "syscall.Munmap(b)"
	}
}

func resSrc(data []byte) string {
	var b strings.Builder
	b.WriteString("package p\n\nimport (\n\"sync\"\n\"syscall\"\n)\n\ntype R struct {\nn int\n}\n\nfunc (R) Close() error { return nil }\n\nfunc (R) Use() {}\n\nfunc sink(any) {}\n\nfunc F(r R, p *sync.Pool, b []byte, e syscall.Errno) {\n_ = e\n")
	for _, op := range resDecode(data) {
		b.WriteString(resStmt(op))
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func resWrite(t *testing.T, dir, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fuzzp\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("write p.go: %v", err)
	}
}

func resLoad(t *testing.T, dir string) []*packages.Package {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  dir,
		Env: append(os.Environ(),
			"GO111MODULE=on",
			"GOFLAGS=-mod=mod",
			"GOPROXY=off",
			"GOTOOLCHAIN=local",
			"GOWORK=off",
		),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("want one package, got %d", len(pkgs))
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatalf("package has errors")
	}
	return pkgs
}

func resCheck(t *testing.T, src string) {
	t.Helper()
	dir := t.TempDir()
	resWrite(t, dir, src)
	pkgs := resLoad(t, dir)
	var graph *checker.Graph
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("analyze panic: %v", r)
			}
		}()
		graph, err = checker.Analyze(resstate.Analyzers(), pkgs, nil)
	}()
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
		if len(root.Diagnostics) > 0 {
			d := root.Diagnostics[0]
			t.Fatalf("%s: unexpected diagnostic at %s: %s", root.Analyzer.Name, pkgs[0].Fset.Position(d.Pos), d.Message)
		}
	}
}

func FuzzNoPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{1})
	f.Add([]byte{0, 1})
	f.Add([]byte{0, 1, 2})
	f.Add([]byte{3, 4, 5})
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{5, 4, 3, 2, 1, 0, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		resCheck(t, resSrc(data))
	})
}
