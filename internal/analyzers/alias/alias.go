package alias

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the four alias checks.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{appendclobber, mapcopy, rangemutate, rangeref}
}

var appendclobber = &analysis.Analyzer{
	Name:     "appendclobber",
	Doc:      "reports appends that clobber a slice read later; covers only appendclobber, mapcopy, rangemutate, rangeref",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runAppendclobber,
}

var mapcopy = &analysis.Analyzer{
	Name:     "mapcopy",
	Doc:      "reports writes to a copy of a map element; covers only appendclobber, mapcopy, rangemutate, rangeref",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runMapcopy,
}

var rangemutate = &analysis.Analyzer{
	Name:     "rangemutate",
	Doc:      "reports appends to a slice while ranging over it; covers only appendclobber, mapcopy, rangemutate, rangeref",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runRangemutate,
}

var rangeref = &analysis.Analyzer{
	Name:     "rangeref",
	Doc:      "reports addresses of a range copy that escape; covers only appendclobber, mapcopy, rangemutate, rangeref",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runRangeref,
}

func eachFunc(pass *analysis.Pass, fn func(ast.Node)) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		n := cur.Node()
		if flow.Body(n) == nil {
			continue
		}
		fn(n)
	}
}

func varOf(info *types.Info, id *ast.Ident) (*types.Var, bool) {
	if v, ok := info.Defs[id].(*types.Var); ok {
		return v, true
	}
	if v, ok := info.Uses[id].(*types.Var); ok {
		return v, true
	}
	return nil, false
}

func identVar(info *types.Info, e ast.Expr) (*types.Var, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil, false
	}
	return varOf(info, id)
}

func isAppend(info *types.Info, e ast.Expr) (*ast.CallExpr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil, false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return nil, false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	if !ok || b.Name() != "append" {
		return nil, false
	}
	return call, true
}

func passesPointer(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if _, ok := e.(*ast.StarExpr); ok {
		return true
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if passesPointer(info, sel.X) {
		return true
	}
	t := info.TypeOf(sel.X)
	if t == nil {
		return false
	}
	if _, ok := t.(*types.Pointer); ok {
		return true
	}
	if _, ok := t.Underlying().(*types.Pointer); ok {
		return true
	}
	return false
}
