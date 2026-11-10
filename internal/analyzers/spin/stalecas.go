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
	Doc:      "report a CAS loop retrying with the same old local when the loop never assigns old, never passes it by address, and never calls a reloading closure; spin covers five shapes only",
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
	if assignsOldInLoop(pass.TypesInfo, loop, old) {
		return
	}
	if loopReceivesAddr(pass.TypesInfo, fn, loop, old) {
		return
	}
	if loopCallsFuncValue(pass.TypesInfo, loop) && funcLitAssignsOld(pass.TypesInfo, fn, old) {
		return
	}
	pass.Reportf(loop.Pos(), "after one failed swap %s is stale, so retries fail until another goroutine stores %s back: load it again inside the loop", old.Name(), old.Name())
}

func assignsOldInLoop(info *types.Info, loop *ast.ForStmt, old *types.Var) bool {
	if loop.Cond != nil && assignsRoot(info, loop.Cond, old) {
		return true
	}
	if loop.Body != nil && assignsRoot(info, loop.Body, old) {
		return true
	}
	return loop.Post != nil && assignsRoot(info, loop.Post, old)
}

func isAddrOfOld(info *types.Info, e ast.Expr, old *types.Var) bool {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return false
	}
	return hitRoot(info, u.X, old)
}

func addrAliases(info *types.Info, fn ast.Node, old *types.Var) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	if fn == nil {
		return out
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		collectAddrAlias(info, n, old, out)
		return true
	})
	return out
}

func collectAddrAlias(info *types.Info, n ast.Node, old *types.Var, out map[*types.Var]bool) {
	switch t := n.(type) {
	case *ast.AssignStmt:
		collectAssignAlias(info, t, old, out)
	case *ast.ValueSpec:
		collectSpecAlias(info, t, old, out)
	}
}

func collectAssignAlias(info *types.Info, t *ast.AssignStmt, old *types.Var, out map[*types.Var]bool) {
	for i, lhs := range t.Lhs {
		if i >= len(t.Rhs) || !isAddrOfOld(info, t.Rhs[i], old) {
			continue
		}
		if id := rootIdent(lhs); id != nil {
			if v, ok := info.ObjectOf(id).(*types.Var); ok {
				out[v] = true
			}
		}
	}
}

func collectSpecAlias(info *types.Info, t *ast.ValueSpec, old *types.Var, out map[*types.Var]bool) {
	for i, name := range t.Names {
		if i >= len(t.Values) || !isAddrOfOld(info, t.Values[i], old) {
			continue
		}
		if v, ok := info.Defs[name].(*types.Var); ok {
			out[v] = true
		}
	}
}

func loopReceivesAddr(info *types.Info, fn ast.Node, loop *ast.ForStmt, old *types.Var) bool {
	aliases := addrAliases(info, fn, old)
	found := false
	walkLoopParts(loop, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok && callTakesAddr(info, call, old, aliases) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callTakesAddr(info *types.Info, call *ast.CallExpr, old *types.Var, aliases map[*types.Var]bool) bool {
	for _, arg := range call.Args {
		if isAddrOfOld(info, arg, old) {
			return true
		}
		if id := rootIdent(arg); id != nil {
			if v, ok := info.ObjectOf(id).(*types.Var); ok && aliases[v] {
				return true
			}
		}
	}
	return false
}

func walkLoopParts(loop *ast.ForStmt, f func(ast.Node) bool) {
	if loop.Cond != nil {
		walkNoLit(loop.Cond, f)
	}
	if loop.Body != nil {
		walkNoLit(loop.Body, f)
	}
	if loop.Post != nil {
		walkNoLit(loop.Post, f)
	}
}

func isFuncValueCall(info *types.Info, call *ast.CallExpr) bool {
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		obj := info.ObjectOf(fn)
		_, isVar := obj.(*types.Var)
		return isVar
	case *ast.SelectorExpr:
		obj := info.ObjectOf(fn.Sel)
		_, isVar := obj.(*types.Var)
		return isVar
	case *ast.FuncLit:
		return true
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	default:
		return false
	}
}

func loopCallsFuncValue(info *types.Info, loop *ast.ForStmt) bool {
	found := false
	walkLoopParts(loop, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok && isFuncValueCall(info, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func funcLitAssignsOld(info *types.Info, fn ast.Node, old *types.Var) bool {
	found := false
	if fn == nil {
		return false
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && assignsRoot(info, lit, old) {
			found = true
			return false
		}
		return true
	})
	return found
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
