package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

func checkValueSites(pass *analysis.Pass, fn inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		checkOneValueSite(pass, fn, cur, f, hash, reported)
	}
}

func checkOneValueSite(pass *analysis.Pass, fn, cur inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	as := inspection.As[*ast.AssignStmt](cur.Node())
	if as.Tok != token.DEFINE && as.Tok != token.ASSIGN {
		return
	}
	if allBlank(as.Lhs) {
		return
	}
	call, vobj, vname := valueSite(pass, as)
	if call == nil {
		return
	}
	if reported[call.Pos()] {
		return
	}
	if isExempt(pass, call, f.never, hash) {
		return
	}
	if !valueHolds(pass, fn, as, vobj, f.safe) {
		return
	}
	callee := types.ExprString(call.Fun)
	pass.Reportf(call.Pos(), "%s is used after the error of %s is dropped, and it may be nil when the call fails: check the error before using %s", vname, callee, vname)
	reported[call.Pos()] = true
}

func allBlank(lhs []ast.Expr) bool {
	for _, e := range lhs {
		if !isBlank(e) {
			return false
		}
	}
	return len(lhs) > 0
}

func valueSite(pass *analysis.Pass, as *ast.AssignStmt) (*ast.CallExpr, *types.Var, string) {
	info := pass.TypesInfo
	if len(as.Rhs) != 1 {
		return nil, nil, ""
	}
	call := callOf(as.Rhs[0])
	if call == nil || !isRealCall(info, call) {
		return nil, nil, ""
	}
	res := resultTypes(info, call)
	if len(res) != len(as.Lhs) {
		return nil, nil, ""
	}
	errIdx := blankErrorIndex(res, as.Lhs)
	if errIdx < 0 {
		return nil, nil, ""
	}
	return qualifyingLHSVar(info, as, errIdx)
}

func blankErrorIndex(res []types.Type, lhs []ast.Expr) int {
	for i := range res {
		if isError(res[i]) && isBlank(lhs[i]) {
			return i
		}
	}
	return -1
}

func qualifyingLHSVar(info *types.Info, as *ast.AssignStmt, errIdx int) (*ast.CallExpr, *types.Var, string) {
	call := callOf(as.Rhs[0])
	for j := range as.Lhs {
		if j == errIdx || isBlank(as.Lhs[j]) {
			continue
		}
		v, name, ok := lhsNamedVar(info, as.Lhs[j])
		if !ok {
			continue
		}
		if !qualifyingValueType(v.Type()) {
			continue
		}
		return call, v, name
	}
	return nil, nil, ""
}

func lhsNamedVar(info *types.Info, e ast.Expr) (*types.Var, string, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, "", false
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, "", false
	}
	return v, id.Name, true
}

func qualifyingValueType(t types.Type) bool {
	if t == nil || isError(t) {
		return false
	}
	switch types.Unalias(t).Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	}
	return false
}

func valueHolds(pass *analysis.Pass, fn inspector.Cursor, as *ast.AssignStmt, v *types.Var, safe map[*types.Func]bool) bool {
	first, ok := firstUseAfter(pass, fn, v, as.End())
	if !ok {
		return false
	}
	if !isDeref(pass, first, v, safe) {
		return false
	}
	if interveningNilCheck(pass, fn, v, as.End(), first.Node().Pos()) {
		return false
	}
	return !interveningAssign(pass, fn, v, as.End(), first.Node().Pos())
}

func firstUseAfter(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, after token.Pos) (inspector.Cursor, bool) {
	var best inspector.Cursor
	bestPos := token.NoPos
	found := false
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if id.Pos() <= after {
			continue
		}
		if pass.TypesInfo.Uses[id] != v {
			continue
		}
		if !found || id.Pos() < bestPos {
			best = cur
			bestPos = id.Pos()
			found = true
		}
	}
	return best, found
}

func skipParens(cur inspector.Cursor) inspector.Cursor {
	for {
		if _, ok := cur.Node().(*ast.ParenExpr); !ok {
			return cur
		}
		cur = cur.Parent()
	}
}

func isDeref(pass *analysis.Pass, cur inspector.Cursor, v *types.Var, safe map[*types.Func]bool) bool {
	id, ok := cur.Node().(*ast.Ident)
	if !ok {
		return false
	}
	up := skipParens(cur.Parent())
	switch p := up.Node().(type) {
	case *ast.SelectorExpr:
		if !unparenIdentEquals(p.X, id) {
			return false
		}
		if valueRecvNonderef(pass, p) {
			return false
		}
		return !isNilSafeCall(pass, up, p, safe)
	case *ast.StarExpr:
		return unparenIdentEquals(p.X, id)
	case *ast.CallExpr:
		fun, ok := ast.Unparen(p.Fun).(*ast.Ident)
		return ok && fun == id
	case *ast.IndexExpr:
		return isMapStoreAssign(up, p, v, id)
	}
	return false
}

func isNilSafeCall(pass *analysis.Pass, up inspector.Cursor, sel *ast.SelectorExpr, safe map[*types.Func]bool) bool {
	gp := skipParens(up.Parent())
	call, ok := gp.Node().(*ast.CallExpr)
	if !ok || ast.Unparen(call.Fun) != up.Node() {
		return false
	}
	fn, ok := directMethod(pass, sel)
	if !ok {
		return false
	}
	if !isPointerRecv(fn) {
		return false
	}
	return hasNilSafe(pass, originOf(fn), safe)
}

func directMethod(pass *analysis.Pass, sel *ast.SelectorExpr) (*types.Func, bool) {
	choice, ok := pass.TypesInfo.Selections[sel]
	if !ok || choice == nil || len(choice.Index()) > 1 {
		return nil, false
	}
	fn, ok := choice.Obj().(*types.Func)
	if !ok {
		return nil, false
	}
	return fn, true
}

func isPointerRecv(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	_, isPtr := sig.Recv().Type().(*types.Pointer)
	return isPtr
}

func valueRecvNonderef(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	fn, ok := directMethod(pass, sel)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	if _, isPtr := sig.Recv().Type().(*types.Pointer); isPtr {
		return false
	}
	switch types.Unalias(sig.Recv().Type()).Underlying().(type) {
	case *types.Map, *types.Chan, *types.Signature:
		return true
	}
	return false
}

func unparenIdentEquals(e ast.Expr, id *ast.Ident) bool {
	xid, ok := ast.Unparen(e).(*ast.Ident)
	return ok && xid == id
}

func isMapStoreAssign(up inspector.Cursor, p *ast.IndexExpr, v *types.Var, id *ast.Ident) bool {
	if !unparenIdentEquals(p.X, id) {
		return false
	}
	if _, ok := types.Unalias(v.Type()).Underlying().(*types.Map); !ok {
		return false
	}
	gp := skipParens(up.Parent())
	as, ok := gp.Node().(*ast.AssignStmt)
	if !ok {
		return false
	}
	return lhsStoresIndex(as, up, p)
}

func lhsStoresIndex(as *ast.AssignStmt, up inspector.Cursor, p *ast.IndexExpr) bool {
	for _, lhs := range as.Lhs {
		if ast.Unparen(lhs) == up.Node() {
			return true
		}
		if idx, ok := ast.Unparen(lhs).(*ast.IndexExpr); ok && idx == p {
			return true
		}
	}
	return false
}

func interveningNilCheck(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, from, to token.Pos) bool {
	for cur := range fn.Preorder((*ast.BinaryExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		b := inspection.As[*ast.BinaryExpr](cur.Node())
		if b.Op != token.EQL && b.Op != token.NEQ {
			continue
		}
		if b.Pos() <= from || b.Pos() >= to {
			continue
		}
		if nilCompareWith(pass.TypesInfo, b, v) {
			return true
		}
	}
	return false
}

func nilCompareWith(info *types.Info, b *ast.BinaryExpr, v *types.Var) bool {
	return sideIsVarAndOtherNil(info, b.X, b.Y, v) || sideIsVarAndOtherNil(info, b.Y, b.X, v)
}

func sideIsVarAndOtherNil(info *types.Info, side, other ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(side).(*ast.Ident)
	if !ok {
		return false
	}
	if info.Uses[id] != v && info.Defs[id] != v {
		return false
	}
	oid, ok := ast.Unparen(other).(*ast.Ident)
	if !ok || oid.Name != nilName {
		return false
	}
	_, ok = info.Uses[oid].(*types.Nil)
	return ok
}

func interveningAssign(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, from, to token.Pos) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if nodeInWindow(cur.Node(), from, to) && nodeAssignsVar(pass.TypesInfo, cur.Node(), v) {
			return true
		}
	}
	return false
}

func nodeInWindow(n ast.Node, from, to token.Pos) bool {
	return n.Pos() > from && n.Pos() < to
}

func nodeAssignsVar(info *types.Info, n ast.Node, v *types.Var) bool {
	switch t := n.(type) {
	case *ast.AssignStmt:
		for _, lhs := range t.Lhs {
			if resolvesTo(info, lhs, []*types.Var{v}) {
				return true
			}
		}
	case *ast.IncDecStmt:
		if resolvesTo(info, t.X, []*types.Var{v}) {
			return true
		}
	case *ast.RangeStmt:
		return rangeAssigns(info, t, []*types.Var{v})
	}
	return false
}
