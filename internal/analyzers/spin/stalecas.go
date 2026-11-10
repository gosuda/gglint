package spin

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var stalecasAnalyzer = &analysis.Analyzer{
	Name:     "stalecas",
	Doc:      "report compare-and-swap loops retrying with a stale value; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runStalecas,
}

func runStalecas(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkStalecas(pass, cur, loop)
	})
	return nil, nil
}

func checkStalecas(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) {
	old, ok := staleShape(pass, loop)
	if !ok {
		return
	}
	fn := enclosingFunc(cur)
	if !isLocalVar(fn, old) || definedInBody(loop, old) {
		return
	}
	if assignsRoot(pass.TypesInfo, loop.Body, old) {
		return
	}
	if loop.Post != nil && assignsRoot(pass.TypesInfo, loop.Post, old) {
		return
	}
	pass.Reportf(loop.Pos(), "after one failed swap %s is stale, so every retry fails: load it again inside the loop", old.Name())
}

func staleShape(pass *analysis.Pass, loop *ast.ForStmt) (*types.Var, bool) {
	if cond, ok := ast.Unparen(loop.Cond).(*ast.UnaryExpr); ok && cond.Op == token.NOT {
		if old, ok := casOld(pass.TypesInfo, cond.X); ok {
			return old, true
		}
	}
	check, ok := firstCheck(loop)
	if !ok {
		return nil, false
	}
	old, ok := casOld(pass.TypesInfo, check.Cond)
	if !ok || !exitsLoop(pass.TypesInfo, check) {
		return nil, false
	}
	return old, true
}

func definedInBody(loop *ast.ForStmt, old *types.Var) bool {
	return loop.Body != nil && loop.Body.Pos() <= old.Pos() && old.Pos() < loop.Body.End()
}

func firstCheck(loop *ast.ForStmt) (*ast.IfStmt, bool) {
	if loop.Body == nil || len(loop.Body.List) == 0 {
		return nil, false
	}
	check, ok := loop.Body.List[0].(*ast.IfStmt)
	if !ok {
		return nil, false
	}
	return check, true
}

func exitsLoop(info *types.Info, check *ast.IfStmt) bool {
	if len(check.Body.List) != 1 {
		return false
	}
	switch t := check.Body.List[0].(type) {
	case *ast.BranchStmt:
		if t.Tok != token.BREAK {
			return false
		}
	case *ast.ReturnStmt:
	default:
		return false
	}
	return !elseLeaves(info, check)
}

func elseLeaves(info *types.Info, check *ast.IfStmt) bool {
	if check.Else == nil {
		return false
	}
	switch e := check.Else.(type) {
	case *ast.BlockStmt:
		return blockLeaves(info, e)
	case *ast.IfStmt:
		return ifLeaves(info, e)
	default:
		return false
	}
}

func ifLeaves(info *types.Info, check *ast.IfStmt) bool {
	if check.Body == nil || !blockLeaves(info, check.Body) {
		return false
	}
	return elseLeaves(info, check)
}

func blockLeaves(info *types.Info, block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	return stmtLeaves(info, block.List[len(block.List)-1])
}

func stmtLeaves(info *types.Info, stmt ast.Stmt) bool {
	switch t := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return t.Tok == token.BREAK || t.Tok == token.GOTO
	case *ast.ExprStmt:
		return isPanicCall(info, t.X)
	case *ast.BlockStmt:
		return blockLeaves(info, t)
	case *ast.IfStmt:
		return ifLeaves(info, t)
	default:
		return false
	}
}

func isPanicCall(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.ObjectOf(id).(*types.Builtin)
	return ok && b.Name() == "panic"
}

func casOld(info *types.Info, e ast.Expr) (*types.Var, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	old, ok := casOldArg(info, call, sel)
	if !ok {
		return nil, false
	}
	id, ok := ast.Unparen(old).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	return v, true
}

func casOldArg(info *types.Info, call *ast.CallExpr, sel *ast.SelectorExpr) (ast.Expr, bool) {
	switch len(call.Args) {
	case 3:
		return atomicOld(info, sel, call.Args[1])
	case 2:
		if !isPkgMethod(info, sel, "sync/atomic", "CompareAndSwap") {
			return nil, false
		}
		return call.Args[0], true
	default:
		return nil, false
	}
}

func atomicOld(info *types.Info, sel *ast.SelectorExpr, arg ast.Expr) (ast.Expr, bool) {
	fn, ok2 := info.ObjectOf(sel.Sel).(*types.Func)
	if !ok2 || fn.Pkg() == nil || fn.Pkg().Path() != "sync/atomic" {
		return nil, false
	}
	if !strings.HasPrefix(fn.Name(), "CompareAndSwap") {
		return nil, false
	}
	return arg, true
}

func assignsRoot(info *types.Info, n ast.Node, v *types.Var) bool {
	hit := false
	walkNoLit(n, func(x ast.Node) bool {
		if assignsNode(info, x, v) {
			hit = true
		}
		return !hit
	})
	return hit
}

func assignsNode(info *types.Info, n ast.Node, v *types.Var) bool {
	switch t := n.(type) {
	case *ast.AssignStmt:
		return lhsHits(info, t.Lhs, v)
	case *ast.IncDecStmt:
		return hitRoot(info, t.X, v)
	case *ast.RangeStmt:
		return rangeHits(info, t, v)
	default:
		return false
	}
}

func lhsHits(info *types.Info, lhs []ast.Expr, v *types.Var) bool {
	for _, e := range lhs {
		if hitRoot(info, e, v) {
			return true
		}
	}
	return false
}

func rangeHits(info *types.Info, t *ast.RangeStmt, v *types.Var) bool {
	if t.Tok != token.ASSIGN {
		return false
	}
	return hitRoot(info, t.Key, v) || hitRoot(info, t.Value, v)
}

func hitRoot(info *types.Info, e ast.Expr, v *types.Var) bool {
	id := rootIdent(e)
	if id == nil {
		return false
	}
	return info.ObjectOf(id) == types.Object(v)
}
