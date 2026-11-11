package chanstate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/chanstate"
)

func fuzzOps() []string {
	return []string{"close", "send", "recv", "remake", "range", "spawn", "selrecv", "loop"}
}

func decodeFuzzOps(data []byte) []string {
	ops := fuzzOps()
	n := min(len(data), 8)
	out := make([]string, 0, n)
	for _, b := range data[:n] {
		out = append(out, ops[int(b)%len(ops)])
	}
	return out
}

func fuzzRange() string {
	return "for v := range ch { _ = v; break }"
}

func fuzzSpawn() string {
	return "go func() { ch <- 1 }()"
}

func fuzzSelRecv() string {
	return "select { case v := <-ch: _ = v; default: _ = 1 }"
}

func fuzzLoop() string {
	return "for i := 0; i < 2; i++ { go func() { ch <- i }() }"
}

func fuzzLine(op string) string {
	switch op {
	case "close":
		return "close(ch)"
	case "send":
		return "ch <- 1"
	case "recv":
		return "<-ch"
	case "remake":
		return "ch = make(chan int, 1)"
	case "range":
		return fuzzRange()
	case "spawn":
		return fuzzSpawn()
	case "selrecv":
		return fuzzSelRecv()
	}
	return fuzzLoop()
}

func renderFuzzSrc(ops []string) string {
	lines := []string{"package p", "", "func f(ch chan int) {"}
	for _, op := range ops {
		lines = append(lines, fuzzLine(op))
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
	graph, err := checker.Analyze(chanstate.Analyzers(), []*packages.Package{pkg}, nil)
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
	f.Add([]byte{0, 0})
	f.Add([]byte{0, 1})
	f.Add([]byte{2})
	f.Add([]byte{1, 2})
	f.Add([]byte{4})
	f.Add([]byte{5, 6})
	f.Add([]byte{7, 2})
	f.Add([]byte{3})
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		checkFuzzSrc(t, renderFuzzSrc(decodeFuzzOps(data)))
	})
}
