package spin

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var busywaitAnalyzer = &analysis.Analyzer{
	Name:     "busywait",
	Doc:      "report try-lock loops that spin instead of blocking; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runBusywait,
}

func runBusywait(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(_ inspector.Cursor, loop *ast.ForStmt) {
		checkBusywait(pass, loop)
	})
	return nil, nil
}

func checkBusywait(pass *analysis.Pass, loop *ast.ForStmt) {
	cond, ok := ast.Unparen(loop.Cond).(*ast.UnaryExpr)
	if !ok || cond.Op != token.NOT {
		return
	}
	call, ok := ast.Unparen(cond.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	var mate string
	switch sel.Sel.Name {
	case "TryLock":
		mate = "Lock"
	case "TryRLock":
		mate = "RLock"
	default:
		return
	}
	if !isPkgMethod(pass.TypesInfo, sel, "sync", sel.Sel.Name) {
		return
	}
	if loop.Body == nil || len(loop.Body.List) > 1 {
		return
	}
	if len(loop.Body.List) == 1 && !isGosched(pass.TypesInfo, loop.Body.List[0]) {
		return
	}
	recv := types.ExprString(ast.Unparen(sel.X))
	pass.Reportf(loop.Pos(), "this loop spins until %s is free: call %s.%s", recv, recv, mate)
}

func isGosched(info *types.Info, stmt ast.Stmt) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(expr.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := info.ObjectOf(sel.Sel).(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "runtime" && fn.Name() == "Gosched"
}
