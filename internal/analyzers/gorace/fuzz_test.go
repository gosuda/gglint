package gorace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/gorace"
)

func stmtFor(v byte) string {
	switch v % 6 {
	case 0:
		return "_ = x\n"
	case 1:
		return "x++\n"
	case 2:
		return "_ = m[0]\n"
	case 3:
		return "mu.Lock()\nm[0] = x\nmu.Unlock()\n"
	case 4:
		return "go func(a int, b map[int]int, c *sync.Mutex) {\n_ = a\n_ = b[0]\nc.Lock()\nb[0] = a\nc.Unlock()\n}(x, m, mu)\n"
	case 5:
		return "wg.Add(1)\ngo func(w *sync.WaitGroup) {\ndefer w.Done()\n}(wg)\nwg.Wait()\n"
	}
	return "_ = x\n"
}

func renderFuzz(data []byte) string {
	var b strings.Builder
	b.WriteString("package fuzz\n\nimport \"sync\"\n\nfunc FuzzFn(x int, m map[int]int, mu *sync.Mutex, wg *sync.WaitGroup) {\n")
	n := len(data)
	if n > 8 {
		n = 8
	}
	for i := range n {
		b.WriteString(stmtFor(data[i]))
	}
	b.WriteString("_, _, _, _ = x, m, mu, wg\n}\n")
	return b.String()
}

func loadFuzz(t *testing.T, src string) *packages.Package {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fuzzmod\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fuzz.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("write fuzz.go: %v", err)
	}
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
	if len(pkgs) != 1 {
		t.Fatalf("load: want one package, got %d", len(pkgs))
	}
	if len(pkgs[0].Errors) != 0 {
		t.Fatalf("load: package errors: %v", pkgs[0].Errors)
	}
	return pkgs[0]
}

func checkSilent(t *testing.T, pkg *packages.Package) {
	t.Helper()
	graph, err := checker.Analyze(gorace.Analyzers(), []*packages.Package{pkg}, nil)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
		if len(root.Diagnostics) != 0 {
			t.Fatalf("%s: %d diagnostics, want zero: %v", root.Analyzer.Name, len(root.Diagnostics), root.Diagnostics)
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
	f.Add([]byte{5, 4, 3, 2, 1, 0})
	f.Add([]byte{3, 3, 4, 4, 5, 5, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		src := renderFuzz(data)
		pkg := loadFuzz(t, src)
		checkSilent(t, pkg)
	})
}
