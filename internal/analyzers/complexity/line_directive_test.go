package complexity_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/complexity"
	"github.com/gosuda/gglint/internal/config"
)

func TestFunctionLineLimitsUsePhysicalLines(t *testing.T) {
	t.Parallel()
	forms := []struct {
		name   string
		header string
		label  string
		column int
	}{
		{"declaration", "func F() {", "function F", 6},
		{"literal", "var F = func() {", "function literal", 9},
	}
	cases := []struct {
		name      string
		prefix    string
		body      string
		wantLines int
		wantFile  string
		wantLine  int
		columnAdd int
	}{
		{
			name: "at limit",
			body: "\t// first\n\t// second\n}",
		},
		{
			name:      "above limit",
			body:      "\t// first\n\t// second\n\t// third\n}",
			wantLines: 5,
			wantFile:  "input.go",
			wantLine:  3,
		},
		{
			name: "forward mapping",
			body: "//line virtual.go:500:7\n\t// counted\n}",
		},
		{
			name:      "backward mapping",
			prefix:    "//line virtual.go:500:7\n",
			body:      "//line virtual.go:1\n\t// first\n\t// second\n}",
			wantLines: 5,
			wantFile:  "virtual.go",
			wantLine:  500,
			columnAdd: 6,
		},
		{
			name: "inline mapping",
			body: "\t/*line virtual.go:500:7*/\n\t// counted\n}",
		},
	}
	for _, form := range forms {
		for _, tc := range cases {
			t.Run(form.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				source := "package p\n\n" + tc.prefix + form.header + "\n" + tc.body + "\n"
				fset := token.NewFileSet()
				file, err := parser.ParseFile(fset, "input.go", source, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				var diagnostics []analysis.Diagnostic
				pass := &analysis.Pass{
					Fset:      fset,
					Files:     []*ast.File{file},
					TypesInfo: &types.Info{},
					Report:    func(d analysis.Diagnostic) { diagnostics = append(diagnostics, d) },
				}
				if _, err := complexity.New(config.Complexity{MaxLines: 4}).Run(pass); err != nil {
					t.Fatal(err)
				}
				if tc.wantLines == 0 {
					if len(diagnostics) != 0 {
						t.Fatalf("diagnostics = %v, want none", diagnostics)
					}
					return
				}
				if len(diagnostics) != 1 {
					t.Fatalf("diagnostics = %v, want one line-count finding", diagnostics)
				}
				want := fmt.Sprintf("%s is too complex: line count %d exceeds 4", form.label, tc.wantLines)
				if diagnostics[0].Message != want {
					t.Errorf("message = %q, want %q", diagnostics[0].Message, want)
				}
				pos := fset.Position(diagnostics[0].Pos)
				if pos.Filename != tc.wantFile || pos.Line != tc.wantLine || pos.Column != form.column+tc.columnAdd {
					t.Errorf("position = %s, want %s:%d:%d", pos, tc.wantFile, tc.wantLine, form.column+tc.columnAdd)
				}
			})
		}
	}
}
