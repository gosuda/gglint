package gorace

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func fanoutOrdered(pass *analysis.Pass, s flow.Spawn, accessPos token.Pos, buffered map[flow.Key]bool) bool {
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
	return capSemaphoreOrders(pass, s, o, accessPos) || capTokenOrders(pass, s, o, accessPos)
}

func capSemaphoreOrders(pass *analysis.Pass, s flow.Spawn, o litOrder, accessPos token.Pos) bool {
	for _, k := range o.sendBeforeKeys {
		if slices.Contains(o.recvAfterKeys, k) && chanCapIsOne(pass, k) && straightSendAcquire(pass, s.Body, k, accessPos) {
			return true
		}
	}
	return false
}

func capTokenOrders(pass *analysis.Pass, s flow.Spawn, o litOrder, accessPos token.Pos) bool {
	for _, k := range o.recvBeforeKeys {
		if slices.Contains(o.sendAfterKeys, k) && chanCapIsOne(pass, k) && straightTokenAcquire(pass, s.Body, k, accessPos) {
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

func nodeContains(outer, inner ast.Node) bool {
	if outer == nil || inner == nil {
		return false
	}
	return outer.Pos() <= inner.Pos() && inner.End() <= outer.End()
}

func crosses(graph *flow.Graph, from, to ast.Node, waits []syncOp) bool {
	if graph == nil || from == nil || to == nil || len(waits) == 0 {
		return false
	}
	avoid := func(n ast.Node) bool {
		for _, w := range waits {
			if nodeContains(n, w.node) {
				return true
			}
		}
		return false
	}
	return !graph.Reaches(from, to, avoid)
}

func entryOrdered(s waitSide, waits []syncOp) bool {
	body := s.root
	if body == nil || len(body.List) == 0 {
		return false
	}
	first := entryStmt(body.List[0])
	for _, w := range waits {
		if isWaitStmt(first, w.node) {
			return true
		}
	}
	return false
}

func entryStmt(stmt ast.Stmt) ast.Stmt {
	var init ast.Stmt
	switch st := stmt.(type) {
	case *ast.IfStmt:
		init = st.Init
	case *ast.SwitchStmt:
		init = st.Init
	case *ast.TypeSwitchStmt:
		init = st.Init
	case *ast.ForStmt:
		init = st.Init
	}
	if init == nil {
		return stmt
	}
	return init
}

func isWaitStmt(stmt, op ast.Node) bool {
	switch stmt.(type) {
	case *ast.SendStmt, *ast.ExprStmt, *ast.AssignStmt, *ast.DeclStmt:
		return nodeContains(stmt, op)
	}
	return false
}

type orderEnv struct {
	pass      *analysis.Pass
	fn        ast.Node
	graph     *flow.Graph
	litGraph  *flow.Graph
	spawn     flow.Spawn
	litOps    []syncOp
	parentOps []syncOp
	buffered  map[syncOpKey]bool
	heldAtGo  map[syncOpKey]bool
}

func aliasHolds(env orderEnv, holder, contained syncOpKey) bool {
	v, ok := holder.root.(*types.Var)
	if !ok || v == nil {
		return false
	}
	alias := receiverAlias(env.pass, env.fn, v)
	if alias == nil {
		return false
	}
	resolved := syncOpKey{root: alias, path: holder.path}
	return syncHolds(resolved, contained)
}

func syncHolds(holder, contained syncOpKey) bool {
	if holder.root == nil || contained.root == nil || holder.root != contained.root {
		return false
	}
	if holder.path == "" || holder.path == contained.path {
		return true
	}
	if len(contained.path) <= len(holder.path) {
		return false
	}
	if contained.path[:len(holder.path)] != holder.path {
		return false
	}
	return contained.path[len(holder.path)] == '.'
}

func forwardOrdered(env orderEnv, ga, pa ast.Node) bool {
	signals := signalsAfter(env.litOps, env.litGraph, ga)
	for _, sig := range signals {
		buf := env.buffered[sig.key]
		waits := waitsFor(env, env.parentOps, sig.key)
		if len(waits) == 0 {
			continue
		}
		if mutexBaton(env, sig, waits, pa) {
			return true
		}
		if checkSide(sig, waits, buf, keepNonLock, parentSide(env, env.spawn.Site, pa)) {
			return true
		}
	}
	return false
}

func backwardOrdered(env orderEnv, ga, pa ast.Node) bool {
	signals := signalsAfter(env.parentOps, env.graph, pa)
	for _, sig := range signals {
		buf := env.buffered[sig.key]
		waits := waitsFor(env, env.litOps, sig.key)
		if len(waits) == 0 {
			continue
		}
		if checkSide(sig, waits, buf, keepNonLockUnlock, litSide(env, ga)) {
			return true
		}
	}
	return false
}

func mutexBaton(env orderEnv, sig syncOp, waits []syncOp, pa ast.Node) bool {
	if sig.kind != kindUnlock {
		return false
	}
	if !env.heldAtGo[sig.key] {
		return false
	}
	var locks []syncOp
	for _, w := range waits {
		if w.kind == kindLock {
			locks = append(locks, w)
		}
	}
	if len(locks) == 0 {
		return false
	}
	if !crosses(env.graph, env.spawn.Site, pa, locks) {
		return false
	}
	s := parentSide(env, env.spawn.Site, pa)
	return !escapedIn(s, locks)
}

func tokenGuard(env orderEnv, ga, pa ast.Node) bool {
	seen := map[syncOpKey]bool{}
	for _, o := range env.litOps {
		if !isChanKey(env, o.key) {
			continue
		}
		if seen[o.key] {
			continue
		}
		seen[o.key] = true
		if bracketedOn(env.litOps, litSide(env, ga), o.key) &&
			bracketedOn(env.parentOps, parentSide(env, flow.Body(env.fn), pa), o.key) {
			return true
		}
	}
	return false
}

func isChanKey(env orderEnv, key syncOpKey) bool {
	t := syncKeyType(env.pass.TypesInfo, flow.Key{Root: key.root, Path: key.path})
	return isChanType(t)
}

func opOnKey(o syncOp, key syncOpKey) bool {
	return o.key.same(key) || (o.kind == kindOpaque && syncHolds(o.key, key))
}

func opsOnKey(ops []syncOp, key syncOpKey) []syncOp {
	var out []syncOp
	for _, o := range ops {
		if opOnKey(o, key) {
			out = append(out, o)
		}
	}
	return out
}

func hasOpAfter(ops []syncOp, graph *flow.Graph, access ast.Node, key syncOpKey) bool {
	for _, o := range opsOnKey(ops, key) {
		if o.deferred {
			return true
		}
		if graph.Reaches(access, o.node, nil) {
			return true
		}
	}
	return false
}

type waitSide struct {
	graph *flow.Graph
	from  ast.Node
	to    ast.Node
	root  *ast.BlockStmt
	keep  func(*ast.SelectStmt) bool
}

func (s waitSide) crosses(waits []syncOp) bool {
	if s.from == ast.Node(s.root) && s.graph.SameNode(s.from, s.to) {
		return false
	}
	return crosses(s.graph, s.from, s.to, waits)
}

func posOf(n ast.Node) token.Pos {
	if n == nil {
		return token.NoPos
	}
	return n.Pos()
}

func keepBetween(from, to ast.Node) func(*ast.SelectStmt) bool {
	after, before := posOf(from), posOf(to)
	return func(sel *ast.SelectStmt) bool { return sel.Pos() > after && before > sel.End() }
}

func keepBefore(ga ast.Node) func(*ast.SelectStmt) bool {
	end := posOf(ga)
	return func(sel *ast.SelectStmt) bool { return sel.Pos() < end }
}

func parentSide(env orderEnv, from, to ast.Node) waitSide {
	return waitSide{
		graph: env.graph, from: from, to: to,
		root: flow.Body(env.fn), keep: keepBetween(from, to),
	}
}

func litSide(env orderEnv, to ast.Node) waitSide {
	if env.spawn.Body == nil {
		return waitSide{graph: env.litGraph, from: nil, to: to, keep: keepBefore(to)}
	}
	return waitSide{
		graph: env.litGraph, from: flow.Body(env.spawn.Body), to: to,
		root: env.spawn.Body.Body, keep: keepBefore(to),
	}
}

func signalsAfter(ops []syncOp, graph *flow.Graph, access ast.Node) []syncOp {
	var out []syncOp
	for _, o := range ops {
		if o.deferred {
			out = append(out, o)
			continue
		}
		if graph.Reaches(access, o.node, nil) {
			out = append(out, o)
		}
	}
	return out
}

func waitsFor(env orderEnv, ops []syncOp, key syncOpKey) []syncOp {
	var out []syncOp
	for _, o := range ops {
		if o.deferred {
			continue
		}
		if opOnKey(o, key) {
			out = append(out, o)
			continue
		}
		if o.kind == kindOpaque && aliasHolds(env, o.key, key) {
			out = append(out, o)
		}
	}
	return out
}

func keepNonLock(k syncKind) bool { return k != kindLock }

func keepNonLockUnlock(k syncKind) bool { return k != kindLock && k != kindUnlock }

func checkSide(sig syncOp, waits []syncOp, buf bool, keep func(syncKind) bool, s waitSide) bool {
	var compat []syncOp
	for _, w := range waits {
		if !keep(w.kind) {
			continue
		}
		if !compatible(sig.kind, w.kind, buf) {
			continue
		}
		compat = append(compat, w)
	}
	if len(compat) == 0 {
		return false
	}
	if !entryOrdered(s, compat) && !s.crosses(compat) {
		return false
	}
	return !escapedIn(s, compat)
}

func escapedIn(s waitSide, waits []syncOp) bool {
	if s.root == nil || len(waits) == 0 {
		return false
	}
	found := false
	ast.Inspect(s.root, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		sel, ok := n.(*ast.SelectStmt)
		if !ok {
			return true
		}
		if s.keep != nil && !s.keep(sel) {
			return true
		}
		if selectEscapes(sel, waits) {
			found = true
			return false
		}
		return true
	})
	return found
}

func selectEscapes(sel *ast.SelectStmt, waits []syncOp) bool {
	if !holdsWaitComm(sel, waits) {
		return false
	}
	for _, cc := range sel.Body.List {
		clause, ok := cc.(*ast.CommClause)
		if !ok {
			continue
		}
		if clauseHasWaitComm(clause, waits) {
			continue
		}
		if bodyWaits(clause, waits) {
			continue
		}
		if flow.EndsInReturn(clause.Body) {
			continue
		}
		return true
	}
	return false
}

func bodyWaits(clause *ast.CommClause, waits []syncOp) bool {
	for _, st := range clause.Body {
		if stmtHoldsWait(st, waits) {
			return true
		}
	}
	return false
}

func stmtHoldsWait(st ast.Node, waits []syncOp) bool {
	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		switch n.(type) {
		case *ast.FuncLit, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			return false
		}
		for _, w := range waits {
			if nodeContains(n, w.node) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func holdsWaitComm(sel *ast.SelectStmt, waits []syncOp) bool {
	for _, cc := range sel.Body.List {
		clause, ok := cc.(*ast.CommClause)
		if !ok {
			continue
		}
		if clauseHasWaitComm(clause, waits) {
			return true
		}
	}
	return false
}

func clauseHasWaitComm(clause *ast.CommClause, waits []syncOp) bool {
	if clause.Comm == nil {
		return false
	}
	for _, w := range waits {
		if nodeContains(clause.Comm, w.node) {
			return true
		}
	}
	return false
}

func bracketedOn(ops []syncOp, s waitSide, key syncOpKey) bool {
	keyed := opsOnKey(ops, key)
	if len(keyed) == 0 {
		return false
	}
	if !s.crosses(keyed) {
		return false
	}
	if escapedIn(s, keyed) {
		return false
	}
	return hasOpAfter(ops, s.graph, s.to, key)
}
