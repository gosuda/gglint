package complexity

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

func New(c config.Complexity) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "complexity",
		Doc:  "complexity reports functions, interfaces, and files that exceed the configured complexity limits.",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, f := range pass.Files {
				if !ast.IsGenerated(f) {
					checkFile(pass, f, c)
				}
			}
			return nil, nil
		},
	}
}

func checkFile(pass *analysis.Pass, f *ast.File, limits config.Complexity) {
	file := pass.Fset.File(f.Package)
	if strings.HasSuffix(file.Name(), "_test.go") {
		limits.MaxLines, limits.MaxFileLines = 0, 0
	}
	if lines := file.LineCount(); over(lines, limits.MaxFileLines) {
		pass.Reportf(f.Package, "file is too long: line count %d exceeds %d", lines, limits.MaxFileLines)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		checkNode(pass, n, limits)
		return true
	})
}

func checkNode(pass *analysis.Pass, n ast.Node, limits config.Complexity) {
	switch n := n.(type) {
	case *ast.FuncDecl:
		if n.Body != nil {
			m := funcMetrics(pass.Fset, pass.TypesInfo, n)
			reportFunc(pass, n.Name.Pos(), n.Body.End(), "function "+n.Name.Name, exceeded(m, limits))
		}
	case *ast.FuncLit:
		m := bodyMetrics(pass.Fset, n.Pos(), n.Type.Params, n.Body)
		reportFunc(pass, n.Pos(), n.Body.End(), "function literal", exceeded(m, limits))
	case *ast.TypeSpec:
		checkInterface(pass, n, limits.MaxInterfaceMethods)
	}
}

func reportFunc(pass *analysis.Pass, pos, end token.Pos, name string, problems []string) {
	if len(problems) == 0 {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:     pos,
		End:     end,
		Message: name + " is too complex: " + strings.Join(problems, ", "),
	})
}

func checkInterface(pass *analysis.Pass, spec *ast.TypeSpec, limit int) {
	iface, ok := spec.Type.(*ast.InterfaceType)
	if !ok || spec.Assign.IsValid() {
		return
	}
	methods := 0
	for _, f := range iface.Methods.List {
		methods += len(f.Names)
	}
	if over(methods, limit) {
		pass.Reportf(spec.Name.Pos(), "interface %s is too large: method count %d exceeds %d", spec.Name.Name, methods, limit)
	}
}

func exceeded(m metrics, limits config.Complexity) []string {
	checks := []struct {
		label      string
		got, limit int
	}{
		{"nesting depth", m.depth, limits.MaxNesting},
		{"parameter count", m.params, limits.MaxParams},
		{"line count", m.lines, limits.MaxLines},
		{"cognitive score", m.score, limits.MaxCognitive},
		{"cyclomatic complexity", m.cyclomatic, limits.MaxCyclomatic},
		{"bool parameter count", m.boolParams, limits.MaxBoolParams},
	}
	var problems []string
	for _, c := range checks {
		if over(c.got, c.limit) {
			problems = append(problems, fmt.Sprintf("%s %d exceeds %d", c.label, c.got, c.limit))
		}
	}
	return problems
}

func over(got, limit int) bool {
	return limit > 0 && got > limit
}
