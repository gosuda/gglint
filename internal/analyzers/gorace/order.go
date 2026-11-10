package gorace

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func spawnCallArgs(s flow.Spawn) []ast.Expr {
	if gs, ok := s.Site.(*ast.GoStmt); ok && gs.Call != nil {
		return gs.Call.Args
	}
	return nil
}

func resolveSpawnParam(pass *analysis.Pass, s flow.Spawn, k flow.Key) flow.Key {
	if s.Body == nil || s.Body.Type == nil || s.Body.Type.Params == nil {
		return k
	}
	root, ok := k.Root.(*types.Var)
	if !ok {
		return k
	}
	idx := paramIndex(pass, s.Body, root)
	if idx < 0 {
		return k
	}
	args := spawnCallArgs(s)
	if idx >= len(args) {
		return k
	}
	ak, ok := flow.KeyOf(pass.TypesInfo, args[idx])
	if !ok {
		return k
	}
	return flow.Key{Root: ak.Root, Path: ak.Path + k.Path}
}

func paramIndex(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) int {
	idx := 0
	for _, f := range lit.Type.Params.List {
		for _, n := range f.Names {
			if pass.TypesInfo.Defs[n] == v {
				return idx
			}
			idx++
		}
	}
	return -1
}

func mappedKeys(pass *analysis.Pass, s flow.Spawn, keys []flow.Key) []flow.Key {
	out := make([]flow.Key, 0, len(keys))
	for _, k := range keys {
		out = append(out, resolveSpawnParam(pass, s, k))
	}
	return out
}

func mappedOrder(pass *analysis.Pass, s flow.Spawn, o litOrder) litOrder {
	o.recvBeforeKeys = mappedKeys(pass, s, o.recvBeforeKeys)
	o.recvAfterKeys = mappedKeys(pass, s, o.recvAfterKeys)
	o.sendBeforeKeys = mappedKeys(pass, s, o.sendBeforeKeys)
	o.sendAfterKeys = mappedKeys(pass, s, o.sendAfterKeys)
	o.closeAfterKeys = mappedKeys(pass, s, o.closeAfterKeys)
	o.helperSendKeys = mappedKeys(pass, s, o.helperSendKeys)
	o.unlockAfterKeys = mappedKeys(pass, s, o.unlockAfterKeys)
	return o
}

func fanoutOrdered(pass *analysis.Pass, fn ast.Node, s flow.Spawn, accessPos token.Pos, buffered map[flow.Key]bool) bool {
	if s.Body == nil {
		return false
	}
	o := mappedOrder(pass, s, scanLitOrder(pass, s.Body, accessPos))
	if o.loadBefore && o.storeAfter {
		return true
	}
	if recvBeforeUnbuffered(o, buffered) && sendAfterUnbuffered(o, buffered) {
		return true
	}
	return capSemaphoreOrders(pass, fn, s, o, accessPos) || capTokenOrders(pass, fn, s, o, accessPos)
}

func capSemaphoreOrders(pass *analysis.Pass, fn ast.Node, s flow.Spawn, o litOrder, accessPos token.Pos) bool {
	for _, k := range o.sendBeforeKeys {
		if slices.Contains(o.recvAfterKeys, k) && chanCapIsOne(pass, fn, k) && straightSendAcquire(pass, s.Body, k, accessPos) {
			return true
		}
	}
	return false
}

func capTokenOrders(pass *analysis.Pass, fn ast.Node, s flow.Spawn, o litOrder, accessPos token.Pos) bool {
	for _, k := range o.recvBeforeKeys {
		if slices.Contains(o.sendAfterKeys, k) && chanCapIsOne(pass, fn, k) && straightTokenAcquire(pass, s.Body, k, accessPos) {
			return true
		}
	}
	return false
}

func straightSendAcquire(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key, accessPos token.Pos) bool {
	return hasStraightSendBefore(pass, lit, k, accessPos) && hasStraightRecvAfter(pass, lit, k, accessPos)
}

func straightTokenAcquire(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key, accessPos token.Pos) bool {
	return hasStraightRecvBefore(pass, lit, k, accessPos) && hasStraightSendAfter(pass, lit, k, accessPos)
}

func hasStraightSendBefore(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key, accessPos token.Pos) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.SendStmt)
		if !ok || st.Pos() >= accessPos {
			return true
		}
		ck, ok := flow.KeyOf(pass.TypesInfo, st.Chan)
		if !ok || ck != k {
			return true
		}
		if straightOrder(lit.Body, st.Pos(), accessPos) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasStraightRecvAfter(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key, accessPos token.Pos) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.ARROW || u.Pos() <= accessPos {
			return true
		}
		ck, ok := flow.KeyOf(pass.TypesInfo, u.X)
		if !ok || ck != k {
			return true
		}
		if straightOrder(lit.Body, accessPos, u.Pos()) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasStraightRecvBefore(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key, accessPos token.Pos) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.ARROW || u.Pos() >= accessPos {
			return true
		}
		ck, ok := flow.KeyOf(pass.TypesInfo, u.X)
		if !ok || ck != k {
			return true
		}
		if straightOrder(lit.Body, u.Pos(), accessPos) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasStraightSendAfter(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key, accessPos token.Pos) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.SendStmt)
		if !ok || st.Pos() <= accessPos {
			return true
		}
		ck, ok := flow.KeyOf(pass.TypesInfo, st.Chan)
		if !ok || ck != k {
			return true
		}
		if straightOrder(lit.Body, accessPos, st.Pos()) {
			found = true
			return false
		}
		return true
	})
	return found
}

func straightOrder(body *ast.BlockStmt, firstPos, secondPos token.Pos) bool {
	aList, aIdx, aOwner := innermostList(body, firstPos)
	if aList == nil {
		return false
	}
	bList, bIdx, bOwner := innermostList(body, secondPos)
	if bList == nil {
		return false
	}
	if aOwner != bOwner {
		return false
	}
	_ = aList
	_ = bList
	return aIdx < bIdx
}

func innermostList(body *ast.BlockStmt, pos token.Pos) ([]ast.Stmt, int, ast.Node) {
	var bestList []ast.Stmt
	bestIdx := -1
	var bestOwner ast.Node
	bestSpan := token.Pos(1 << 30)
	ast.Inspect(body, func(n ast.Node) bool {
		list := stmtList(n)
		if list == nil {
			return true
		}
		for i, st := range list {
			if st.Pos() <= pos && pos < st.End() {
				span := st.End() - st.Pos()
				if span < bestSpan {
					bestSpan = span
					bestList = list
					bestIdx = i
					bestOwner = n
				}
				break
			}
		}
		return true
	})
	return bestList, bestIdx, bestOwner
}

func stmtList(n ast.Node) []ast.Stmt {
	switch t := n.(type) {
	case *ast.BlockStmt:
		return t.List
	case *ast.CommClause:
		return t.Body
	case *ast.CaseClause:
		return t.Body
	}
	return nil
}

func (m *sharedScope) callsBetween(s flow.Spawn, at ast.Node, k flow.Key) bool {
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool {
		if selectIgnores(m.fn, n, at) {
			return false
		}
		return nodeCallWaitsOn(m.pass, m.fn, n, k)
	})
}

func nodeCallWaitsOn(pass *analysis.Pass, fn, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callWaitsOn(pass, fn, call, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callWaitsOn(pass *analysis.Pass, fn ast.Node, call *ast.CallExpr, k flow.Key) bool {
	if calleeReceivesMapped(pass, call, k) {
		return true
	}
	if calleeVisible(pass, call) {
		return false
	}
	return callMentionsKey(pass, fn, call, k)
}

func calleeVisible(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || pass.Pkg == nil {
		return false
	}
	if fn.Pkg() != pass.Pkg {
		return false
	}
	return funcDeclFor(pass, fn) != nil
}

func calleeReceivesMapped(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || pass.Pkg == nil {
		return false
	}
	if fn.Pkg() != pass.Pkg {
		return false
	}
	decl := funcDeclFor(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	sub := receiverSubst(pass, call, decl)
	return bodyReceivesMapped(pass, decl.Body, sub, k)
}

func bodyReceivesMapped(pass *analysis.Pass, body *ast.BlockStmt, sub map[*types.Var]flow.Key, k flow.Key) bool {
	found := false
	ast.Inspect(body, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if recvMatches(pass, sub, x, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func recvMatches(pass *analysis.Pass, sub map[*types.Var]flow.Key, x ast.Node, k flow.Key) bool {
	u, ok := x.(*ast.UnaryExpr)
	if !ok || u.Op != token.ARROW {
		return false
	}
	ck, ok := flow.KeyOf(pass.TypesInfo, u.X)
	if !ok {
		return false
	}
	return substKey(sub, ck) == k
}

func receiverSubst(pass *analysis.Pass, call *ast.CallExpr, decl *ast.FuncDecl) map[*types.Var]flow.Key {
	out := map[*types.Var]flow.Key{}
	addRecvSubst(pass, call, decl, out)
	addParamSubst(pass, call, decl, out)
	return out
}

func addRecvSubst(pass *analysis.Pass, call *ast.CallExpr, decl *ast.FuncDecl, out map[*types.Var]flow.Key) {
	if decl.Recv == nil {
		return
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	rk, ok := flow.KeyOf(pass.TypesInfo, sel.X)
	if !ok {
		return
	}
	for _, f := range decl.Recv.List {
		for _, n := range f.Names {
			if v, ok := pass.TypesInfo.Defs[n].(*types.Var); ok {
				out[v] = rk
			}
		}
	}
}

func addParamSubst(pass *analysis.Pass, call *ast.CallExpr, decl *ast.FuncDecl, out map[*types.Var]flow.Key) {
	if decl.Type == nil || decl.Type.Params == nil {
		return
	}
	idx := 0
	for _, f := range decl.Type.Params.List {
		for _, n := range f.Names {
			idx = addOneParam(pass, call, n, idx, out)
		}
	}
}

func addOneParam(pass *analysis.Pass, call *ast.CallExpr, n *ast.Ident, idx int, out map[*types.Var]flow.Key) int {
	if idx >= len(call.Args) {
		return idx + 1
	}
	if v, ok := pass.TypesInfo.Defs[n].(*types.Var); ok {
		if ak, ok := flow.KeyOf(pass.TypesInfo, call.Args[idx]); ok {
			out[v] = ak
		}
	}
	return idx + 1
}

func substKey(sub map[*types.Var]flow.Key, k flow.Key) flow.Key {
	root, ok := k.Root.(*types.Var)
	if !ok {
		return k
	}
	base, ok := sub[root]
	if !ok {
		return k
	}
	return flow.Key{Root: base.Root, Path: base.Path + k.Path}
}

func callMentionsKey(pass *analysis.Pass, fn ast.Node, call *ast.CallExpr, k flow.Key) bool {
	aliases := varAliases(pass, fn)
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if mentionsExpr(pass, aliases, sel.X, k) {
			return true
		}
	}
	for _, a := range call.Args {
		if mentionsExpr(pass, aliases, a, k) {
			return true
		}
	}
	return false
}

func mentionsExpr(pass *analysis.Pass, aliases map[*types.Var]*types.Var, e ast.Expr, k flow.Key) bool {
	ck, ok := flow.KeyOf(pass.TypesInfo, e)
	if !ok {
		return false
	}
	if ck == k {
		return true
	}
	cr, ok := ck.Root.(*types.Var)
	if !ok {
		return false
	}
	kr, ok := k.Root.(*types.Var)
	if !ok {
		return false
	}
	if cr == kr {
		return true
	}
	if aliases[cr] == kr {
		return true
	}
	if aliases[kr] == cr {
		return true
	}
	return false
}

func varAliases(pass *analysis.Pass, fn ast.Node) map[*types.Var]*types.Var {
	out := map[*types.Var]*types.Var{}
	if fn == nil {
		return out
	}
	body := flow.Body(fn)
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != ast.Node(body) {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
		}
		switch t := n.(type) {
		case *ast.AssignStmt:
			aliasAssign(pass, t.Lhs, t.Rhs, out)
		case *ast.ValueSpec:
			aliasValue(pass, t.Names, t.Values, out)
		}
		return true
	})
	return out
}

func aliasAssign(pass *analysis.Pass, lhs, rhs []ast.Expr, out map[*types.Var]*types.Var) {
	if len(lhs) != len(rhs) {
		return
	}
	for i := range lhs {
		aliasPair(pass, lhs[i], rhs[i], out)
	}
}

func aliasValue(pass *analysis.Pass, names []*ast.Ident, values []ast.Expr, out map[*types.Var]*types.Var) {
	if len(values) == 0 {
		return
	}
	if len(names) != len(values) && len(values) != 1 {
		return
	}
	for i, n := range names {
		v := values[0]
		if len(values) == len(names) {
			v = values[i]
		}
		aliasPair(pass, n, v, out)
	}
}

func aliasPair(pass *analysis.Pass, lhs, rhs ast.Expr, out map[*types.Var]*types.Var) {
	lid, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	rid, ok := ast.Unparen(rhs).(*ast.Ident)
	if !ok {
		return
	}
	lv, ok := pass.TypesInfo.Defs[lid].(*types.Var)
	if !ok {
		if lv, ok = pass.TypesInfo.Uses[lid].(*types.Var); !ok {
			return
		}
	}
	var rv *types.Var
	if r, ok := pass.TypesInfo.Defs[rid].(*types.Var); ok {
		rv = r
	} else if r, ok := pass.TypesInfo.Uses[rid].(*types.Var); ok {
		rv = r
	} else {
		return
	}
	out[lv] = rv
}

func (m *sharedScope) joinedBroken(s flow.Spawn, at ast.Node, o litOrder) bool {
	keys := append(slices.Clone(o.closeAfterKeys), o.sendAfterKeys...)
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if !m.breakingSelect(s.Site, at, k) {
			return false
		}
	}
	return true
}

func (m *sharedScope) breakingSelect(site, at ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(m.fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok && n != m.fn {
			return false
		}
		sel, ok := n.(*ast.SelectStmt)
		if !ok {
			return true
		}
		if !m.selectBetween(site, at, sel) {
			return true
		}
		if sel.Pos() <= at.Pos() && at.Pos() < sel.End() {
			return true
		}
		if m.selectBreaks(sel, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func (m *sharedScope) selectBetween(site, at ast.Node, sel *ast.SelectStmt) bool {
	if !m.graph.Reaches(site, sel, nil) {
		return false
	}
	return m.graph.Reaches(sel, at, nil)
}

func (m *sharedScope) selectBreaks(sel *ast.SelectStmt, k flow.Key) bool {
	for _, cc := range sel.Body.List {
		c, ok := cc.(*ast.CommClause)
		if !ok {
			continue
		}
		if c.Comm == nil {
			return true
		}
		if !nodeRecvsOn(m.pass, c.Comm, k) {
			return true
		}
	}
	return false
}
