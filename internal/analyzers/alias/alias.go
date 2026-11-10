package alias

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the four alias checks.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{appendclobber, mapcopy, rangemutate, rangeref}
}

var appendclobber = &analysis.Analyzer{
	Name:     "appendclobber",
	Doc:      "reports appends that clobber a slice read later; covers only appendclobber, mapcopy, rangemutate, rangeref",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
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
		fn(cur.Node())
	}
}

func bodyOf(fn ast.Node) *ast.BlockStmt {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		return fn.Body
	case *ast.FuncLit:
		return fn.Body
	}
	return nil
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

func usesVar(info *types.Info, root ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if info.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}
