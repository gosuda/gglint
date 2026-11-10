package spin_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/spin"
)

func fuzzLoop(b byte) string {
	switch b % 8 {
	case 0:
		return "for { v := <-ch; use(v) }\n"
	case 1:
		return "for { select { case v := <-ch: use(v); default: time.Sleep(d) } }\n"
	case 2:
		return "for { select { case v := <-ch: use(v) } }\n"
	case 3:
		return "for !mu.TryLock() { time.Sleep(d) }\n"
	case 4:
		return "for { old := n.Load(); if n.CompareAndSwap(old, old+1) { break } }\n"
	case 5:
		return "for i := 0; i < 3; i++ { if ctx.Err() != nil { continue } }\n"
	case 6:
		return "{ i := 0; for i < 3 { i++; if ctx.Err() != nil { continue } } }\n"
	default:
		return "for i := 0; i < 3; i++ { use(i) }\n"
	}
}

func fuzzSource(data []byte) string {
	var sb strings.Builder
	sb.WriteString("package fuzz\n\nimport (\n\"context\"\n\"sync\"\n\"sync/atomic\"\n\"time\"\n)\n\nfunc use(any) {}\n\nfunc FuzzFn(ch chan int, mu *sync.Mutex, n *atomic.Int64, d time.Duration, ctx context.Context) {\n_ = mu\n_ = n\n_ = d\n_ = ctx\n")
	m := len(data)
	if m > 4 {
		m = 4
	}
	for _, b := range data[:m] {
		sb.WriteString(fuzzLoop(b))
	}
	sb.WriteString("}\n")
	return sb.String()
}

func checkFuzzShapes(t *testing.T, data []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fuzzmod\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fuzz.go"), []byte(fuzzSource(data)), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &packages.Config{
		Dir: dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
		Env: append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("want one package, got %d", len(pkgs))
	}
	graph, err := checker.Analyze(spin.Analyzers(), []*packages.Package{pkgs[0]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
		if len(root.Diagnostics) != 0 {
			t.Fatalf("%s: want no diagnostics, got %v", root.Analyzer.Name, root.Diagnostics)
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
	f.Add([]byte{6})
	f.Add([]byte{7})
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		checkFuzzShapes(t, data)
	})
}
