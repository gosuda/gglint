package errdrop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/errdrop"
)

func FuzzNoPanic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add([]byte{1})
	f.Add([]byte{2})
	f.Add([]byte{3})
	f.Add([]byte{4})
	f.Add([]byte{5})
	f.Add([]byte{0, 3})
	f.Add([]byte{4, 5, 0})
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzErrdrop(t, data)
	})
}

func fuzzErrdrop(t *testing.T, data []byte) {
	t.Helper()
	checkErrdropSilent(t, renderErrdrop(decodeErrdrop(data)))
}

func decodeErrdrop(data []byte) []string {
	ops := []string{
		"c0()",
		"_ = c0()",
		"if err := c0(); err != nil { return err }",
		"c2(&p)",
		"p, _ = c1()",
		"r.Close()",
	}
	n := min(len(data), 8)
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, ops[int(data[i])%len(ops)])
	}
	return out
}

func renderErrdrop(stmts []string) string {
	var b strings.Builder
	b.WriteString("package p\n\ntype R struct{}\n\nfunc (R) Close() error { return nil }\n\nfunc F(c0 func() error, c1 func() (int, error), c2 func(*int) error, r R, p int) error {\n")
	for _, s := range stmts {
		b.WriteString(s)
		b.WriteString("\n")
	}
	b.WriteString("return c0()\n}\n")
	return b.String()
}

func checkErrdropSilent(t *testing.T, src string) {
	t.Helper()
	dir := t.TempDir()
	writeErrdropFile(t, filepath.Join(dir, "go.mod"), "module fuzzp\n\ngo 1.27\n")
	writeErrdropFile(t, filepath.Join(dir, "p.go"), src)
	pkgs := loadErrdropPackage(t, dir)
	graph, err := checker.Analyze([]*analysis.Analyzer{errdrop.Analyzer}, pkgs, nil)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
		if len(root.Diagnostics) > 0 {
			d := root.Diagnostics[0]
			t.Fatalf("unexpected diagnostic at %s: %s", pkgs[0].Fset.Position(d.Pos), d.Message)
		}
	}
}

func writeErrdropFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func loadErrdropPackage(t *testing.T, dir string) []*packages.Package {
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
		t.Fatalf("load %s: %v", dir, err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("load %s: want one package, got %d", dir, len(pkgs))
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatalf("load %s: package has errors", dir)
	}
	return pkgs
}
