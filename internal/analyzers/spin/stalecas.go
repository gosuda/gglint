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
	Doc:      "reports a loop for !v.CompareAndSwap(old, new) (and the other shapes it already recognises) when old is a local, nothing in the loop assigns old, no goto or labeled break or continue leaves the loop, no other literal assigns old or a field or element stored in it, and the address of old or of such a field or element never appears; the last two are function-wide, so such loops are not reported; a path through a pointer does not count; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runStalecas,
}

func runStalecas(pass *analysis.Pass) (any, error) {
	cache := map[ast.Node]*staleFn{}
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkStalecas(pass, cur, loop, cache)
	})
	return nil, nil
}

type staleFn struct {
	addrs  map[*types.Var]bool
	lits   []*ast.FuncLit
	labels []*ast.LabeledStmt
}

func checkStalecas(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt, cache map[ast.Node]*staleFn) {
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
	fc := staleFnFor(pass.TypesInfo, fn, cache)
	if loopHasOutsideJump(pass.TypesInfo, fc, loop, old) {
		return
	}
	if fc.addrs[old] {
		return
	}
	if otherLitAssignsOld(pass.TypesInfo, fc, loop, old) {
		return
	}
	pass.Reportf(loop.Pos(), "after one failed swap %s is stale, so retries fail until another goroutine stores %s back: load it again inside the loop", old.Name(), old.Name())
}

func staleFnFor(info *types.Info, fn ast.Node, cache map[ast.Node]*staleFn) *staleFn {
	if fc, ok := cache[fn]; ok {
		return fc
	}
	fc := &staleFn{addrs: map[*types.Var]bool{}}
	cache[fn] = fc
	if fn == nil {
		return fc
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		collectStaleNode(info, fc, n)
		return true
	})
	return fc
}

func collectStaleNode(info *types.Info, fc *staleFn, n ast.Node) {
	switch t := n.(type) {
	case *ast.FuncLit:
		fc.lits = append(fc.lits, t)
	case *ast.LabeledStmt:
		fc.labels = append(fc.labels, t)
	case *ast.UnaryExpr:
		noteAddr(info, fc, t)
	}
}

func noteAddr(info *types.Info, fc *staleFn, t *ast.UnaryExpr) {
	if t.Op != token.AND {
		return
	}
	if root, ok := storageRoot(info, t.X); ok {
		fc.addrs[root] = true
	}
}

func storageRoot(info *types.Info, e ast.Expr) (*types.Var, bool) {
	if e == nil || info == nil {
		return nil, false
	}
	switch t := ast.Unparen(e).(type) {
	case *ast.Ident:
		return identRoot(info, t)
	case *ast.SelectorExpr:
		return selectorRoot(info, t)
	case *ast.IndexExpr:
		return arrayRoot(info, t.X)
	case *ast.IndexListExpr:
		return arrayRoot(info, t.X)
	default:
		return nil, false
	}
}

func identRoot(info *types.Info, id *ast.Ident) (*types.Var, bool) {
	if v, ok := info.ObjectOf(id).(*types.Var); ok {
		return v, true
	}
	return nil, false
}

func selectorRoot(info *types.Info, t *ast.SelectorExpr) (*types.Var, bool) {
	inner := ast.Unparen(t.X)
	if isPointerType(info.TypeOf(inner)) {
		return nil, false
	}
	return storageRoot(info, inner)
}

func arrayRoot(info *types.Info, x ast.Expr) (*types.Var, bool) {
	inner := ast.Unparen(x)
	if !isArrayType(info.TypeOf(inner)) {
		return nil, false
	}
	return storageRoot(info, inner)
}

func isPointerType(t types.Type) bool {
	if t == nil {
		return true
	}
	_, ok := t.Underlying().(*types.Pointer)
	return ok
}

func isArrayType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Array)
	return ok
}

func isOldStorage(info *types.Info, e ast.Expr, old *types.Var) bool {
	if e == nil || old == nil {
		return false
	}
	root, ok := storageRoot(info, e)
	return ok && root == old
}

func assignsOldNode(info *types.Info, n ast.Node, old *types.Var) bool {
	switch t := n.(type) {
	case *ast.AssignStmt:
		return assignsLHS(info, t, old)
	case *ast.IncDecStmt:
		return isOldStorage(info, t.X, old)
	case *ast.RangeStmt:
		return assignsRange(info, t, old)
	default:
		return false
	}
}

func assignsLHS(info *types.Info, t *ast.AssignStmt, old *types.Var) bool {
	for _, e := range t.Lhs {
		if !isOldStorage(info, e, old) {
			continue
		}
		if t.Tok == token.DEFINE && definingIdent(e, old) {
			continue
		}
		return true
	}
	return false
}

func definingIdent(e ast.Expr, old *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Pos() == old.Pos()
}

func assignsRange(info *types.Info, t *ast.RangeStmt, old *types.Var) bool {
	if t.Tok != token.ASSIGN {
		return false
	}
	return isOldStorage(info, t.Key, old) || isOldStorage(info, t.Value, old)
}

func otherLitAssignsOld(info *types.Info, fc *staleFn, loop *ast.ForStmt, old *types.Var) bool {
	for _, lit := range fc.lits {
		if lit.Pos() <= loop.Pos() && loop.End() <= lit.End() {
			continue
		}
		if litAssignsOld(info, lit, old) {
			return true
		}
	}
	return false
}

func litAssignsOld(info *types.Info, lit *ast.FuncLit, old *types.Var) bool {
	found := false
	walkNoLit(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if assignsOldNode(info, n, old) {
			found = true
		}
		return !found
	})
	return found
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

func loopHasOutsideJump(info *types.Info, fc *staleFn, loop *ast.ForStmt, old *types.Var) bool {
	if loop.Body == nil {
		return false
	}
	inside := loopLabels(loop.Body)
	own := ownLoopLabels(fc, loop)
	for name := range own {
		inside[name] = true
	}
	enclosing := enclosingLabels(fc, loop)
	var reloadOwn map[string]bool
	if initAssignsOld(info, loop, old) {
		reloadOwn = own
	}
	return scanLeaves(loop.Body, inside, enclosing, reloadOwn)
}

func ownLoopLabels(fc *staleFn, loop *ast.ForStmt) map[string]bool {
	out := map[string]bool{}
	for _, lab := range fc.labels {
		if labelsLoop(lab, loop) {
			out[lab.Label.Name] = true
		}
	}
	return out
}

func initAssignsOld(info *types.Info, loop *ast.ForStmt, old *types.Var) bool {
	if loop.Init == nil {
		return false
	}
	if loop.Init.Pos() <= old.Pos() && old.Pos() < loop.Init.End() {
		return true
	}
	return assignsRoot(info, loop.Init, old)
}

func enclosingLabels(fc *staleFn, loop *ast.ForStmt) map[string]bool {
	out := map[string]bool{}
	for _, lab := range fc.labels {
		if labelsLoop(lab, loop) {
			continue
		}
		if lab.Pos() <= loop.Pos() && loop.End() <= lab.End() {
			out[lab.Label.Name] = true
		}
	}
	return out
}

func scanLeaves(body ast.Node, inside, enclosing, reloadOwn map[string]bool) bool {
	leaves := false
	walkNoLit(body, func(n ast.Node) bool {
		if leaves {
			return false
		}
		if branchLeaves(n, inside, enclosing, reloadOwn) {
			leaves = true
		}
		return !leaves
	})
	return leaves
}

func branchLeaves(n ast.Node, inside, enclosing, reloadOwn map[string]bool) bool {
	b, ok := n.(*ast.BranchStmt)
	if !ok {
		return false
	}
	if b.Tok == token.GOTO {
		if b.Label == nil {
			return false
		}
		if reloadOwn != nil && reloadOwn[b.Label.Name] {
			return true
		}
		return !inside[b.Label.Name]
	}
	if b.Tok == token.BREAK || b.Tok == token.CONTINUE {
		return b.Label != nil && enclosing[b.Label.Name]
	}
	return false
}

func labelsLoop(lab *ast.LabeledStmt, loop *ast.ForStmt) bool {
	s := lab.Stmt
	for s != nil {
		if f, ok := s.(*ast.ForStmt); ok && f == loop {
			return true
		}
		n, ok := s.(*ast.LabeledStmt)
		if !ok {
			return false
		}
		s = n.Stmt
	}
	return false
}

func loopLabels(body ast.Node) map[string]bool {
	inside := map[string]bool{}
	walkNoLit(body, func(n ast.Node) bool {
		if lab, ok := n.(*ast.LabeledStmt); ok {
			inside[lab.Label.Name] = true
		}
		return true
	})
	return inside
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
		if assignsOldNode(info, x, v) {
			hit = true
		}
		return !hit
	})
	return hit
}
