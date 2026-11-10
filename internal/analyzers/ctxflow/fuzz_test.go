package ctxflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/ctxflow"
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
		return "{c0, cancel0 := context.WithTimeout(ctx, 0); defer cancel0(); _ = c0}"
	case 1:
		return "{c1, cancel1 := context.WithCancel(ctx); defer cancel1(); _ = c1}"
	case 2:
		return "_ = context.Background()"
	case 3:
		return "context.TODO()"
	case 4:
		return "select { case <-ctx.Done(): _ = 1; case v := <-ch: _ = v }"
	default:
		return "_, _ = db.QueryContext(ctx, \"SELECT 1\")\n_ = exec.CommandContext(ctx, \"ls\")"
	}
}

func fuzzSrc(data []byte) string {
	var b strings.Builder
	b.WriteString("package p\n\nimport (\n\"context\"\n\"database/sql\"\n\"os/exec\"\n)\n\nvar _ = exec.CommandContext\n\nfunc F(ctx context.Context, ch chan int, db *sql.DB) {\n")
	for _, op := range fuzzOps(data) {
		b.WriteString(fuzzStmt(op))
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func fuzzFiles(src string) map[string]string {
	return map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.27\n",
		"p.go":   src,
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
		graph, err = checker.Analyze(ctxflow.Analyzers(), pkgs, nil)
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
	pkgs, err := packages.Load(cfg, ".")
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
	f.Add([]byte{1})
	f.Add([]byte{2})
	f.Add([]byte{3})
	f.Add([]byte{4})
	f.Add([]byte{5})
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{5, 4, 3, 2, 1, 0, 0, 2})
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		fuzzWrite(t, dir, fuzzSrc(data))
		fuzzCheck(t, dir)
	})
}
