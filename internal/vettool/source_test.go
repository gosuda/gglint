package vettool

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

var packageSources = map[string]string{
	"/work/p/a.go": "package p\n\nvar A = 1\n",
	"/work/p/b.go": "package p\n\n//nolint:reader // b is suppressed\nvar B = 2\n",
	"/work/p/c.go": "package p\n\nvar C = 3\n",
}

func readSource(name string) ([]byte, error) {
	src, ok := packageSources[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(src), nil
}

func multiFilePass(t *testing.T) *analysis.Pass {
	t.Helper()
	pass := &analysis.Pass{Fset: token.NewFileSet(), ReadFile: readSource}
	for _, name := range []string{"/work/p/a.go", "/work/p/b.go", "/work/p/c.go"} {
		f, err := parser.ParseFile(pass.Fset, name, packageSources[name], parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		pass.Files = append(pass.Files, f)
	}
	return pass
}

func readerRule(name string, wrongReads chan<- string) rules.Rule {
	reader := &analysis.Analyzer{
		Name: name,
		Doc:  "reads every file and reports the first declaration of each",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, f := range pass.Files {
				filename := pass.Fset.File(f.FileStart).Name()
				if got, err := pass.ReadFile(filename); err != nil || string(got) != packageSources[filename] {
					wrongReads <- filename
				}
				pass.Reportf(f.Decls[0].Pos(), "declaration")
			}
			if _, err := pass.ReadFile("/work/p/missing.go"); !errors.Is(err, fs.ErrNotExist) {
				wrongReads <- "/work/p/missing.go"
			}
			return nil, nil
		},
	}
	return rules.Rule{Name: name, Build: func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{reader} }}
}

func TestConcurrentAnalyzersReadEachFileOwnBytesAndDirectives(t *testing.T) {
	e := newTestEngine(t, config.Config{Mode: config.Standard, Tests: true})
	e.known["reader"], e.selected["reader"] = true, true
	pass := multiFilePass(t)
	var mu sync.Mutex
	kept := map[string]int{}
	pass.Report = func(d analysis.Diagnostic) {
		mu.Lock()
		defer mu.Unlock()
		kept[pass.Fset.File(d.Pos).Name()]++
	}
	wrongReads := make(chan string, 2*(len(packageSources)+1))
	analyzers := e.analyzers([]rules.Rule{readerRule("reader", wrongReads), readerRule("other", wrongReads)})
	var wg sync.WaitGroup
	for _, a := range analyzers {
		wg.Go(func() {
			if _, err := a.Run(pass); err != nil {
				t.Errorf("run %s: %v", a.Name, err)
			}
		})
	}
	wg.Wait()
	close(wrongReads)
	for name := range wrongReads {
		t.Errorf("ReadFile(%q) did not return that file's own result", name)
	}
	want := map[string]int{"/work/p/a.go": 2, "/work/p/b.go": 1, "/work/p/c.go": 2}
	for file, n := range want {
		if kept[file] != n {
			t.Errorf("%s: kept %d findings, want %d", file, kept[file], n)
		}
	}
}
