package rules

import (
	"go/ast"
	"go/token"
	"maps"

	"github.com/ldez/usetesting"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/config"
)

func buildUsetesting(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{guardBodyless(usetesting.NewAnalyzer())}
}

func guardBodyless(upstream *analysis.Analyzer) *analysis.Analyzer {
	wrapped := *upstream
	run := upstream.Run
	wrapped.Run = func(pass *analysis.Pass) (any, error) {
		files := pass.Files
		next := *pass
		if filtered, ok := withoutBodyless(pass.Files); ok {
			results := make(map[*analysis.Analyzer]any, len(pass.ResultOf)+1)
			maps.Copy(results, pass.ResultOf)
			results[inspect.Analyzer] = inspector.New(filtered)
			next.ResultOf = results
			next.Files = filtered
			files = filtered
		}
		if bodies := brokenFixBodies(files); len(bodies) > 0 {
			report := pass.Report
			next.Report = func(d analysis.Diagnostic) {
				if insideAny(d.Pos, bodies) {
					d.SuggestedFixes = nil
				}
				report(d)
			}
		}
		return run(&next)
	}
	return &wrapped
}

func brokenFixBodies(files []*ast.File) []*ast.BlockStmt {
	var bodies []*ast.BlockStmt
	for _, file := range files {
		bodies = appendFileBrokenBodies(bodies, file)
	}
	return bodies
}

func appendFileBrokenBodies(bodies []*ast.BlockStmt, file *ast.File) []*ast.BlockStmt {
	var units []funcUnit
	ast.Inspect(file, func(n ast.Node) bool {
		var body *ast.BlockStmt
		var ft *ast.FuncType
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body, ft = fn.Body, fn.Type
		case *ast.FuncLit:
			body, ft = fn.Body, fn.Type
		default:
			return true
		}
		if body != nil {
			units = append(units, funcUnit{body: body, typ: ft})
		}
		return true
	})
	for _, u := range units {
		if firstParamBroken(u.typ) && !nestedInTest(u.body, units) {
			bodies = append(bodies, u.body)
		}
	}
	return bodies
}

type funcUnit struct {
	body *ast.BlockStmt
	typ  *ast.FuncType
}

func nestedInTest(body *ast.BlockStmt, units []funcUnit) bool {
	for _, u := range units {
		if u.body != body && testParam(u.typ) != nil && body.Pos() > u.body.Pos() && body.End() <= u.body.End() {
			return true
		}
	}
	return false
}

func firstParamBroken(ft *ast.FuncType) bool {
	field := testParam(ft)
	return field != nil && (len(field.Names) == 0 || field.Names[0].Name == "_")
}

func testParam(ft *ast.FuncType) *ast.Field {
	if ft == nil || ft.Params == nil || len(ft.Params.List) == 0 {
		return nil
	}
	field := ft.Params.List[0]
	if !isTestingType(field.Type) {
		return nil
	}
	return field
}

func isTestingType(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == "testing" && (sel.Sel.Name == "T" || sel.Sel.Name == "B")
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "testing" && sel.Sel.Name == "TB"
}

func insideAny(pos token.Pos, bodies []*ast.BlockStmt) bool {
	for _, body := range bodies {
		if pos >= body.Pos() && pos <= body.End() {
			return true
		}
	}
	return false
}

func withoutBodyless(files []*ast.File) ([]*ast.File, bool) {
	filtered := make([]*ast.File, len(files))
	changed := false
	for i, file := range files {
		decls := make([]ast.Decl, 0, len(file.Decls))
		for _, d := range file.Decls {
			if fn, isFunc := d.(*ast.FuncDecl); isFunc && fn.Body == nil {
				continue
			}
			decls = append(decls, d)
		}
		if len(decls) == len(file.Decls) {
			filtered[i] = file
			continue
		}
		changed = true
		clone := *file
		clone.Decls = decls
		filtered[i] = &clone
	}
	if !changed {
		return nil, false
	}
	return filtered, true
}
