package vettool

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

func newPass(t *testing.T, name, src string, report func(analysis.Diagnostic)) *analysis.Pass {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return &analysis.Pass{
		Fset:   fset,
		Files:  []*ast.File{file},
		Report: report,
		ReadFile: func(filename string) ([]byte, error) {
			if filename != name {
				return nil, fs.ErrNotExist
			}
			return []byte(src), nil
		},
	}
}

func newTestEngine(t *testing.T, cfg config.Config) *engine {
	t.Helper()
	e, _, err := newEngine(cfg)
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	return e
}

func probeRule() rules.Rule {
	probe := &analysis.Analyzer{
		Name: "probe",
		Doc:  "reports the first declaration of the first file",
		Run: func(pass *analysis.Pass) (any, error) {
			pass.Reportf(pass.Files[0].Decls[0].Pos(), "probe finding")
			return nil, nil
		},
	}
	return rules.Rule{Name: "probe", Build: func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{probe} }}
}

func TestFilterUsesRealFileNameNotLineDirective(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		file string
		src  string
		want int
	}{
		{
			name: "test file renamed to a non-test file is dropped when tests are off",
			cfg:  config.Config{Mode: config.Standard},
			file: "/work/p/x_test.go",
			src:  "package p\n\n//line x.go:1\nvar X = 1\n",
			want: 0,
		},
		{
			name: "non-test file renamed to a test file is kept when tests are off",
			cfg:  config.Config{Mode: config.Standard},
			file: "/work/p/x.go",
			src:  "package p\n\n//line x_test.go:1\nvar X = 1\n",
			want: 1,
		},
		{
			name: "exclude path matches the real path relative to the exclude base",
			cfg: config.Config{
				Mode:        config.Standard,
				Tests:       true,
				ExcludeBase: "/work",
				Excludes:    []config.Exclude{{Paths: []string{`^p/real\.go$`}}},
			},
			file: "/work/p/real.go",
			src:  "package p\n\n//line /work/p/other.go:1\nvar X = 1\n",
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t, tc.cfg)
			var got []analysis.Diagnostic
			pass := newPass(t, tc.file, tc.src, func(d analysis.Diagnostic) { got = append(got, d) })
			for _, a := range e.analyzers([]rules.Rule{probeRule()}) {
				if _, err := a.Run(pass); err != nil {
					t.Fatalf("run %s: %v", a.Name, err)
				}
			}
			if len(got) != tc.want {
				t.Fatalf("kept %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}
