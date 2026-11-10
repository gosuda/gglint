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
	Doc:      "report for TryLock loops with empty or Gosched-only body when the lock is an identifier or field chain; other bodies are not reported; spin covers five shapes only",
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
	sel, mate, ok := busywaitTarget(pass, loop)
	if !ok {
		return
	}
	if !busywaitSpins(pass, loop) {
		return
	}
	recv := types.ExprString(ast.Unparen(sel.X))
	pass.Reportf(loop.Pos(), "this loop spins until %s is free: call %s.%s", recv, recv, mate)
}

func busywaitTarget(pass *analysis.Pass, loop *ast.ForStmt) (*ast.SelectorExpr, string, bool) {
	call, ok := busywaitCall(loop.Cond)
	if !ok {
		return nil, "", false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, "", false
	}
	mate, ok := tryMate(sel)
	if !ok {
		return nil, "", false
	}
	if !isPkgMethod(pass.TypesInfo, sel, "sync", sel.Sel.Name) {
		return nil, "", false
	}
	if !isLockReceiver(sel.X) {
		return nil, "", false
	}
	return sel, mate, true
}

func isLockReceiver(e ast.Expr) bool {
	e = ast.Unparen(e)
	for {
		star, ok := e.(*ast.StarExpr)
		if !ok {
			break
		}
		e = ast.Unparen(star.X)
	}
	switch t := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return isLockReceiver(t.X)
	default:
		return false
	}
}

func busywaitCall(cond ast.Expr) (*ast.CallExpr, bool) {
	un, ok := ast.Unparen(cond).(*ast.UnaryExpr)
	if !ok || un.Op != token.NOT {
		return nil, false
	}
	call, ok := ast.Unparen(un.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	return call, true
}

func tryMate(sel *ast.SelectorExpr) (string, bool) {
	switch sel.Sel.Name {
	case "TryLock":
		return "Lock", true
	case "TryRLock":
		return "RLock", true
	default:
		return "", false
	}
}

func busywaitSpins(pass *analysis.Pass, loop *ast.ForStmt) bool {
	if loop.Body == nil || len(loop.Body.List) > 1 {
		return false
	}
	if len(loop.Body.List) == 1 && !isGosched(pass.TypesInfo, loop.Body.List[0]) {
		return false
	}
	return true
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
