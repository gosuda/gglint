package vettool

import (
	"go/ast"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

func TestNolintScopesIgnoreLineDirectives(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "reused virtual line",
			src: "package p\n" +
				"//line first.go:2\n" +
				"var Suppressed = 1 //nolint:probe // intentional\n" +
				"//line second.go:2\n" +
				"var Reported = 2\n",
		},
		{
			name: "standalone directive under remapping",
			src: "package p\n" +
				"//line virtual.go:2\n" +
				"//nolint:probe // intentional\n" +
				"var Suppressed = 1\n" +
				"var Reported = 2\n",
		},
		{
			name: "virtual line beyond physical file",
			src: "package p\n" +
				"//line virtual.go:1000\n" +
				"var Suppressed = 1 //nolint:probe // intentional\n" +
				"var Reported = 2\n",
		},
		{
			name: "declaration spans a line reset",
			src: "package p\n" +
				"//nolint:probe // intentional\n" +
				"//line virtual.go:1000\n" +
				"var (\n\tSuppressed = 1\n" +
				"//line virtual.go:1\n" +
				"\tAlsoSuppressed = 2\n)\n" +
				"var Reported = 3\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEngine(t, config.Config{Mode: config.Standard, Tests: true})
			e.known["probe"], e.selected["probe"] = true, true
			var got []string
			pass := newPass(t, "/work/p/a.go", tc.src, func(d analysis.Diagnostic) { got = append(got, d.Message) })
			probe := &analysis.Analyzer{
				Name: "probe",
				Doc:  "reports variable declarations",
				Run: func(pass *analysis.Pass) (any, error) {
					ast.Inspect(pass.Files[0], func(node ast.Node) bool {
						if value, ok := node.(*ast.ValueSpec); ok {
							pass.Reportf(value.Pos(), "probe %s", value.Names[0].Name)
						}
						return true
					})
					return nil, nil
				},
			}
			rule := rules.Rule{Name: "probe", Build: func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{probe} }}
			for _, analyzer := range reportPanics(e.analyzers([]rules.Rule{rule})) {
				if _, err := analyzer.Run(pass); err != nil {
					t.Fatalf("run %s: %v", analyzer.Name, err)
				}
			}
			if _, err := e.nolintAnalyzer(nil).Run(pass); err != nil {
				t.Fatalf("run nolint: %v", err)
			}
			if want := []string{"probe Reported"}; !slices.Equal(got, want) {
				t.Fatalf("reported %q, want %q", got, want)
			}
		})
	}
}
