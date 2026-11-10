package gorace

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type varAccess struct {
	key      flow.Key
	stmt     ast.Node
	write    bool
	index    bool
	del      bool
	rng      bool
	clr      bool
	isMap    bool
	idxConst bool
	idxVal   string
}

type mapScope struct {
	*guardScope
	graph  *flow.Graph
	fn     ast.Node
	parent []varAccess
	lits   map[*ast.FuncLit][]varAccess
}
type sharedScope struct {
	*guardScope
	graph       *flow.Graph
	fn          ast.Node
	parent      []varAccess
	hasWaitCall bool
}

type pairOrder struct {
	avoid    func(ast.Node) bool
	buffered map[flow.Key]bool
	order    litOrder
}

func writesInStmt(info *types.Info, stmt ast.Stmt) []varAccess {
	var out []varAccess
	ast.Inspect(stmt, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		out = appendStmtWrite(info, n, stmt, out)
		return true
	})
	return out
}

func appendStmtWrite(info *types.Info, n ast.Node, stmt ast.Stmt, out []varAccess) []varAccess {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return appendAssignWrites(info, n, out)
	case *ast.IncDecStmt:
		return appendIncWrites(info, n, n, out)
	case *ast.CallExpr:
		return appendDeleteWrite(info, n, stmt, out)
	}
	return out
}

func appendAssignWrites(info *types.Info, n *ast.AssignStmt, out []varAccess) []varAccess {
	for _, lhs := range n.Lhs {
		out = appendLHSWrite(info, lhs, n, out)
	}
	return out
}

func appendLHSWrite(info *types.Info, lhs ast.Expr, stmt ast.Stmt, out []varAccess) []varAccess {
	if idx, ok := ast.Unparen(lhs).(*ast.IndexExpr); ok {
		if k, ok := flow.KeyOf(info, idx.X); ok {
			base := info.TypeOf(idx.X)
			val, has := constIndex(info, idx.Index, base)
			out = append(out, varAccess{key: k, stmt: stmt, write: true, index: true, isMap: isMapType(base), idxConst: has, idxVal: val})
		}
		return out
	}
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := info.Defs[id].(*types.Var); ok {
			return append(out, varAccess{key: flow.Key{Root: v}, stmt: stmt, write: true, isMap: isMapType(v.Type())})
		}
	}
	if k, ok := flow.KeyOf(info, lhs); ok {
		out = append(out, varAccess{key: k, stmt: stmt, write: true, isMap: isMapType(info.TypeOf(lhs))})
	}
	return out
}

func appendIncWrites(info *types.Info, n *ast.IncDecStmt, stmt ast.Stmt, out []varAccess) []varAccess {
	if idx, ok := ast.Unparen(n.X).(*ast.IndexExpr); ok {
		if k, ok := flow.KeyOf(info, idx.X); ok {
			base := info.TypeOf(idx.X)
			val, has := constIndex(info, idx.Index, base)
			out = append(out, varAccess{key: k, stmt: stmt, write: true, index: true, isMap: isMapType(base), idxConst: has, idxVal: val})
		}
		return out
	}
	if k, ok := flow.KeyOf(info, n.X); ok {
		out = append(out, varAccess{key: k, stmt: stmt, write: true, isMap: isMapType(info.TypeOf(n.X))})
	}
	return out
}

func appendDeleteWrite(info *types.Info, n *ast.CallExpr, stmt ast.Stmt, out []varAccess) []varAccess {
	id, ok := ast.Unparen(n.Fun).(*ast.Ident)
	if !ok || len(n.Args) == 0 {
		return out
	}
	clr := isBuiltin(info, id, "clear")
	if !clr && !isBuiltin(info, id, "delete") {
		return out
	}
	if k, ok := flow.KeyOf(info, n.Args[0]); ok {
		out = append(out, varAccess{key: k, stmt: stmt, write: true, del: !clr, clr: clr, isMap: true})
	}
	return out
}

func isBuiltin(info *types.Info, id *ast.Ident, name string) bool {
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
}

func constIndex(info *types.Info, idx ast.Expr, base types.Type) (string, bool) {
	if info == nil || idx == nil {
		return "", false
	}
	if isMapType(base) {
		return "", false
	}
	tv, ok := info.Types[idx]
	if !ok || tv.Value == nil {
		return "", false
	}
	t := deref(base)
	if t == nil {
		return "", false
	}
	switch t.Underlying().(type) {
	case *types.Slice, *types.Array:
		return tv.Value.String(), true
	default:
		return "", false
	}
}

func readsInStmt(info *types.Info, stmt ast.Stmt, writes []varAccess) []varAccess {
	skip := map[flow.Key]bool{}
	for _, w := range writes {
		skip[w.key] = true
	}
	var out []varAccess
	ast.Inspect(stmt, func(n ast.Node) bool {
		handled, descend := appendReadNode(info, n, stmt, skip, &out)
		if handled {
			return descend
		}
		return true
	})
	return out
}

func appendReadNode(info *types.Info, n ast.Node, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) (handled, descend bool) {
	if _, ok := n.(*ast.FuncLit); ok {
		return true, false
	}
	if call, ok := n.(*ast.CallExpr); ok && isLenCall(info, call) {
		return true, false
	}
	if idx, ok := n.(*ast.IndexExpr); ok && appendIndexedRead(info, idx, stmt, skip, out) {
		return true, false
	}
	if appendSelectorRead(info, n, stmt, skip, out) {
		return true, false
	}
	if id, ok := n.(*ast.Ident); ok {
		appendIdentRead(info, id, stmt, skip, out)
	}
	return false, true
}

func appendIndexedRead(info *types.Info, idx *ast.IndexExpr, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) bool {
	k, ok := flow.KeyOf(info, idx.X)
	if !ok {
		return false
	}
	if !skip[k] {
		base := info.TypeOf(idx.X)
		val, has := constIndex(info, idx.Index, base)
		*out = append(*out, varAccess{key: k, stmt: stmt, index: true, isMap: isMapType(base), idxConst: has, idxVal: val})
	}
	appendSubReads(info, idx.Index, stmt, skip, out)
	return true
}

func appendSubReads(info *types.Info, e ast.Expr, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		handled, descend := appendReadNode(info, n, stmt, skip, out)
		if handled {
			return descend
		}
		return true
	})
}

func appendSelectorRead(info *types.Info, n ast.Node, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok || info.Selections[sel] == nil {
		return false
	}
	k, ok := flow.KeyOf(info, sel)
	if ok && !skip[k] {
		*out = append(*out, varAccess{key: k, stmt: stmt, isMap: isMapType(info.TypeOf(sel))})
	}
	return true
}

func appendIdentRead(info *types.Info, id *ast.Ident, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) {
	v, ok := info.Uses[id].(*types.Var)
	if !ok || id.Name == "_" || skip[flow.Key{Root: v}] {
		return
	}
	*out = append(*out, varAccess{key: flow.Key{Root: v}, stmt: stmt, isMap: isMapType(v.Type())})
}

func isLenCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	return isBuiltin(info, id, "len")
}

func stmtAccesses(info *types.Info, stmt ast.Stmt) []varAccess {
	w := writesInStmt(info, stmt)
	return append(w, readsInStmt(info, stmt, w)...)
}

func collectParent(info *types.Info, fn ast.Node, sites map[ast.Node]bool) []varAccess {
	var out []varAccess
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if gs, ok := n.(*ast.GoStmt); ok {
			if !sites[gs] {
				out = append(out, callAccesses(info, gs, gs.Call)...)
			}
			return false
		}
		if ds, ok := n.(*ast.DeferStmt); ok {
			if !sites[ds.Call] {
				out = append(out, callAccesses(info, ds, ds.Call)...)
			}
			return false
		}
		out = appendParentNode(info, n, sites, out)
		return true
	})
	return out
}

func appendParentNode(info *types.Info, n ast.Node, sites map[ast.Node]bool, out []varAccess) []varAccess {
	if next, ok := appendParentLeaf(info, n, sites, out); ok {
		return next
	}
	if next, ok := appendHeaderReads(info, n, out); ok {
		return next
	}
	return out
}

func appendHeaderReads(info *types.Info, n ast.Node, out []varAccess) ([]varAccess, bool) {
	switch n := n.(type) {
	case *ast.IfStmt:
		return append(out, condAccesses(info, n, n.Cond)...), true
	case *ast.ForStmt:
		reads := condAccesses(info, n, n.Cond)
		if n.Cond != nil {
			for i := range reads {
				reads[i].stmt = n.Cond
			}
		}
		return append(out, reads...), true
	case *ast.RangeStmt:
		out = appendRangeReads(info, n, out)
		return append(out, rangeWrites(info, n)...), true
	case *ast.SwitchStmt:
		return append(out, exprReads(info, n, n.Tag)...), true
	case *ast.TypeSwitchStmt:
		return append(out, exprReads(info, n, n.Assign)...), true
	case *ast.CaseClause:
		return appendCaseReads(info, n, out), true
	}
	return out, false
}

func appendRangeReads(info *types.Info, n *ast.RangeStmt, out []varAccess) []varAccess {
	reads := condAccesses(info, n, n.X)
	if isMapType(info.TypeOf(n.X)) {
		for i := range reads {
			reads[i].rng = true
		}
	}
	return append(out, reads...)
}

func appendParentLeaf(info *types.Info, n ast.Node, sites map[ast.Node]bool, out []varAccess) ([]varAccess, bool) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.IncDecStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.ReturnStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.ExprStmt:
		return appendParentExpr(info, n, sites, out), true
	case *ast.DeclStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.SendStmt:
		return append(out, stmtAccesses(info, n)...), true
	}
	return out, false
}

func appendParentExpr(info *types.Info, n *ast.ExprStmt, sites map[ast.Node]bool, out []varAccess) []varAccess {
	if call, ok := ast.Unparen(n.X).(*ast.CallExpr); ok && sites[call] {
		return out
	}
	return append(out, stmtAccesses(info, n)...)
}

func condAccesses(info *types.Info, stmt ast.Stmt, e ast.Expr) []varAccess {
	if e == nil {
		return nil
	}
	var out []varAccess
	ast.Inspect(e, func(n ast.Node) bool {
		handled, descend := appendCondNode(info, n, stmt, &out)
		if handled {
			return descend
		}
		return true
	})
	return out
}

func appendCondNode(info *types.Info, n ast.Node, stmt ast.Stmt, out *[]varAccess) (handled, descend bool) {
	if _, ok := n.(*ast.FuncLit); ok {
		return true, false
	}
	if call, ok := n.(*ast.CallExpr); ok && isLenCall(info, call) {
		return true, false
	}
	if idx, ok := n.(*ast.IndexExpr); ok {
		if k, ok := flow.KeyOf(info, idx.X); ok {
			base := info.TypeOf(idx.X)
			val, has := constIndex(info, idx.Index, base)
			*out = append(*out, varAccess{key: k, stmt: stmt, index: true, isMap: isMapType(base), idxConst: has, idxVal: val})
			*out = append(*out, exprReads(info, stmt, idx.Index)...)
			return true, false
		}
	}
	if sel, ok := n.(*ast.SelectorExpr); ok && info.Selections[sel] != nil {
		appendCondSelector(info, sel, stmt, out)
		return true, false
	}
	if id, ok := n.(*ast.Ident); ok {
		appendCondIdent(info, id, stmt, out)
	}
	return false, true
}

func appendCondSelector(info *types.Info, sel *ast.SelectorExpr, stmt ast.Stmt, out *[]varAccess) {
	if k, ok := flow.KeyOf(info, sel); ok {
		*out = append(*out, varAccess{key: k, stmt: stmt})
	}
}

func appendCondIdent(info *types.Info, id *ast.Ident, stmt ast.Stmt, out *[]varAccess) {
	if v, ok := info.Uses[id].(*types.Var); ok && id.Name != "_" {
		*out = append(*out, varAccess{key: flow.Key{Root: v}, stmt: stmt})
	}
}

func appendCaseReads(info *types.Info, c *ast.CaseClause, out []varAccess) []varAccess {
	for _, e := range c.List {
		out = append(out, condAccesses(info, c, e)...)
	}
	return out
}

func exprReads(info *types.Info, stmt ast.Stmt, e ast.Node) []varAccess {
	var out []varAccess
	if e == nil {
		return out
	}
	ast.Inspect(e, func(n ast.Node) bool {
		handled, descend := appendCondNode(info, n, stmt, &out)
		if handled {
			return descend
		}
		return true
	})
	return out
}

func callAccesses(info *types.Info, stmt ast.Stmt, call *ast.CallExpr) []varAccess {
	out := exprReads(info, stmt, call.Fun)
	for _, a := range call.Args {
		out = append(out, exprReads(info, stmt, a)...)
	}
	return out
}

func rangeWrites(info *types.Info, n *ast.RangeStmt) []varAccess {
	var out []varAccess
	if n.Tok != token.ASSIGN {
		return out
	}
	if n.Key != nil {
		if k, ok := flow.KeyOf(info, n.Key); ok {
			out = append(out, varAccess{key: k, stmt: n, write: true})
		}
	}
	if n.Value != nil {
		if k, ok := flow.KeyOf(info, n.Value); ok {
			out = append(out, varAccess{key: k, stmt: n, write: true})
		}
	}
	return out
}

func collectLit(info *types.Info, lit *ast.FuncLit) []varAccess {
	var out []varAccess
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != lit {
			return false
		}
		out = appendLitNode(info, n, out)
		return true
	})
	return out
}

func appendLitNode(info *types.Info, n ast.Node, out []varAccess) []varAccess {
	if next, ok := appendLitLeaf(info, n, out); ok {
		return next
	}
	if next, ok := appendHeaderReads(info, n, out); ok {
		return next
	}
	switch n := n.(type) {
	case *ast.GoStmt:
		return append(out, callAccesses(info, n, n.Call)...)
	case *ast.DeferStmt:
		return append(out, callAccesses(info, n, n.Call)...)
	}
	return out
}

func appendLitLeaf(info *types.Info, n ast.Node, out []varAccess) ([]varAccess, bool) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.IncDecStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.ExprStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.ReturnStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.DeclStmt:
		return append(out, stmtAccesses(info, n)...), true
	case *ast.SendStmt:
		return append(out, stmtAccesses(info, n)...), true
	}
	return out, false
}

func runMap(pass *analysis.Pass) (any, error) {
	for _, fn := range funcBodies(pass) {
		checkMap(pass, fn)
	}
	return nil, nil
}

func checkMap(pass *analysis.Pass, fn ast.Node) {
	info := pass.TypesInfo
	if passesMutex(info, fn) || flow.Body(fn) == nil {
		return
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	spawns := flow.Spawns(pass, fn)
	sites := map[ast.Node]bool{}
	for _, s := range spawns {
		sites[s.Site] = true
	}
	scope := &mapScope{guardScope: newGuardScope(pass, fn), graph: g, fn: fn, parent: collectParent(info, fn, sites), lits: map[*ast.FuncLit][]varAccess{}}
	for _, s := range spawns {
		if s.Body != nil {
			scope.lits[s.Body] = collectLit(info, s.Body)
		}
	}
	for _, s := range spawns {
		if s.Body == nil {
			continue
		}
		scope.checkSpawn(s, spawns)
	}
}

func (m *mapScope) checkSpawn(s flow.Spawn, spawns []flow.Spawn) {
	if passesMutex(m.pass.TypesInfo, s.Body) {
		return
	}
	for _, a := range m.lits[s.Body] {
		if m.reportableMapWrite(s, a, spawns) {
			m.pass.Reportf(s.Site.Pos(), "%s is a map written by this goroutine and accessed concurrently, which can crash the process: guard every access with one mutex", keyText(m.pass, a.key))
			return
		}
	}
}

func (m *mapScope) reportableMapWrite(s flow.Spawn, a varAccess, spawns []flow.Spawn) bool {
	if !mapReportable(s, a) {
		return false
	}
	outside := declaredOutsideLoop(m.pass.TypesInfo, m.loops, a.key.Root, s.Site)
	if !outside && !m.parentMapOp(a) {
		return false
	}
	litOpen := len(m.litHeld(s.Body, a.stmt)) == 0
	return (outside && s.InLoop && litOpen) || m.hasOther(s, a, spawns)
}

func (m *mapScope) parentMapOp(a varAccess) bool {
	for _, p := range m.parent {
		if p.key == a.key && mapOpAccess(p) {
			return true
		}
	}
	return false
}

func mapOpAccess(p varAccess) bool {
	return p.index || p.del || p.rng || p.clr
}

func mapReportable(s flow.Spawn, a varAccess) bool {
	if !a.write || !a.isMap || (!a.index && !a.del && !a.clr) {
		return false
	}
	v, ok := a.key.Root.(*types.Var)
	if !ok || !outsideLit(v, s.Body) {
		return false
	}
	return true
}

func (m *mapScope) hasOther(s flow.Spawn, a varAccess, spawns []flow.Spawn) bool {
	if len(m.parent) > 0 {
		avoid := declAvoid(m.pass.TypesInfo, m.loops, a.key.Root, s.Site)
		for _, p := range m.parent {
			if m.parentConflicts(s, a, p, avoid) {
				return true
			}
		}
	}
	return m.otherSpawnWrites(s, a, spawns)
}

func (m *mapScope) parentConflicts(s flow.Spawn, a, p varAccess, avoid func(ast.Node) bool) bool {
	if p.key != a.key || !mapOpAccess(p) {
		return false
	}
	if !m.graph.Reaches(s.Site, p.stmt, avoid) || flow.Joined(m.pass, m.graph, s, p.stmt) {
		return false
	}
	return !commonKey(m.litHeld(s.Body, a.stmt), m.parentHeld(p.stmt))
}

func (m *mapScope) otherSpawnWrites(s flow.Spawn, a varAccess, spawns []flow.Spawn) bool {
	for _, o := range spawns {
		if o.Body == nil || o.Body == s.Body || passesMutex(m.pass.TypesInfo, o.Body) {
			continue
		}
		if m.otherLitConflicts(s, a, o) {
			return true
		}
	}
	return false
}

func (m *mapScope) otherLitConflicts(s flow.Spawn, a varAccess, o flow.Spawn) bool {
	lit, ok := m.lits[o.Body]
	if !ok {
		lit = collectLit(m.pass.TypesInfo, o.Body)
	}
	for _, b := range lit {
		if b.key == a.key && mapOpAccess(b) && !commonKey(m.litHeld(s.Body, a.stmt), m.litHeld(o.Body, b.stmt)) {
			return true
		}
	}
	return false
}

func runFanout(pass *analysis.Pass) (any, error) {
	for _, fn := range funcBodies(pass) {
		checkFanout(pass, fn)
	}
	return nil, nil
}

type fanoutCtx struct {
	loops    []loopNest
	buffered map[flow.Key]bool
}

func checkFanout(pass *analysis.Pass, fn ast.Node) {
	info := pass.TypesInfo
	if passesMutex(info, fn) || flow.Body(fn) == nil {
		return
	}
	ctx := fanoutCtx{loops: collectLoops(fn), buffered: bufferedChanKeys(pass, fn)}
	for _, s := range flow.Spawns(pass, fn) {
		if !s.InLoop || s.Body == nil || passesMutex(info, s.Body) || exitsListAfter(fn, s.Site) {
			continue
		}
		for _, a := range collectLit(info, s.Body) {
			reportFanoutWrite(pass, fn, ctx, s, a)
		}
	}
}

func reportFanoutWrite(pass *analysis.Pass, fn ast.Node, ctx fanoutCtx, s flow.Spawn, a varAccess) {
	if !fanoutReportable(pass, fn, ctx, s, a) {
		return
	}
	pass.Reportf(a.stmt.Pos(), "%s is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex", keyText(pass, a.key))
}

func fanoutReportable(pass *analysis.Pass, fn ast.Node, ctx fanoutCtx, s flow.Spawn, a varAccess) bool {
	if !a.write || a.index || a.del || a.clr {
		return false
	}
	v, ok := a.key.Root.(*types.Var)
	if !ok {
		return false
	}
	return fanoutVarReportable(pass, fn, ctx, s, v, a)
}

func fanoutVarReportable(pass *analysis.Pass, fn ast.Node, ctx fanoutCtx, s flow.Spawn, v *types.Var, a varAccess) bool {
	if !outsideLit(v, s.Body) || isSyncType(v.Type()) || !declaredOutsideLoop(pass.TypesInfo, ctx.loops, v, s.Site) {
		return false
	}
	if len(heldOutside(pass, s.Body, lockNodeFor(a))) != 0 {
		return false
	}
	return !fanoutOrdered(pass, fn, s, a.stmt.Pos(), ctx.buffered)
}

func runShared(pass *analysis.Pass) (any, error) {
	for _, fn := range funcBodies(pass) {
		checkShared(pass, fn)
	}
	return nil, nil
}

func checkShared(pass *analysis.Pass, fn ast.Node) {
	info := pass.TypesInfo
	if passesMutex(info, fn) || flow.Body(fn) == nil {
		return
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	spawns := flow.Spawns(pass, fn)
	sites := map[ast.Node]bool{}
	for _, s := range spawns {
		sites[s.Site] = true
	}
	scope := &sharedScope{guardScope: newGuardScope(pass, fn), graph: g, fn: fn, parent: collectParent(info, fn, sites), hasWaitCall: isSynctestWait(pass, flow.Body(fn))}
	buffered := bufferedChanKeys(pass, fn)
	for _, s := range spawns {
		if s.InLoop || s.Body == nil {
			continue
		}
		scope.checkSpawn(s, buffered)
	}
}

func (m *sharedScope) checkSpawn(s flow.Spawn, buffered map[flow.Key]bool) {
	lit := collectLit(m.pass.TypesInfo, s.Body)
	for _, k := range sharedKeys(m.pass, m.fn, s, lit) {
		if m.conflict(s, k, lit, buffered) {
			return
		}
	}
}

func sharedKeys(pass *analysis.Pass, fn ast.Node, s flow.Spawn, lit []varAccess) []flow.Key {
	seen := map[flow.Key]bool{}
	var order []flow.Key
	for _, a := range lit {
		v, ok := a.key.Root.(*types.Var)
		if !ok || !outsideLit(v, s.Body) || isSyncType(v.Type()) || a.isMap || !flow.IsLocal(pass.TypesInfo, fn, v) {
			continue
		}
		if !seen[a.key] {
			seen[a.key] = true
			order = append(order, a.key)
		}
	}
	return order
}

func (m *sharedScope) conflict(s flow.Spawn, k flow.Key, lit []varAccess, buffered map[flow.Key]bool) bool {
	if passesMutex(m.pass.TypesInfo, s.Body) {
		return false
	}
	avoid := declAvoid(m.pass.TypesInfo, m.loops, k.Root, s.Site)
	for _, a := range lit {
		if a.key != k {
			continue
		}
		po := pairOrder{avoid: avoid, buffered: buffered, order: scanLitOrder(m.pass, s.Body, lockNodeFor(a).Pos())}
		for _, p := range m.parent {
			if m.pairVoid(s, a, p, po) {
				continue
			}
			line := m.pass.Fset.Position(p.stmt.Pos()).Line
			m.pass.Reportf(s.Site.Pos(), "%s is %s by this goroutine and %s at line %d before the goroutine is joined: pass it as an argument, or wait for the goroutine first", keyText(m.pass, k), litWord(a.write), parentWord(p.write), line)
			return true
		}
	}
	return false
}

func disjointIndex(a, p varAccess) bool {
	return a.idxConst && p.idxConst && a.idxVal != p.idxVal
}

func (m *sharedScope) pairVoid(s flow.Spawn, a, p varAccess, po pairOrder) bool {
	if p.key != a.key {
		return true
	}
	if !a.write && !p.write {
		return true
	}
	if disjointIndex(a, p) {
		return true
	}
	if !m.graph.Reaches(s.Site, p.stmt, po.avoid) {
		return true
	}
	mapped := mappedOrder(m.pass, s, po.order)
	if m.joinedOrders(s, p.stmt, mapped) {
		return true
	}
	if m.bubbleJoined(s, p.stmt) {
		return true
	}
	if m.handoff(s, p, po) {
		return true
	}
	return commonKey(m.litHeld(s.Body, lockNodeFor(a)), m.parentHeld(lockNodeFor(p)))
}

func (m *sharedScope) joinedOrders(s flow.Spawn, at ast.Node, mapped litOrder) bool {
	if !flow.Joined(m.pass, m.graph, s, at) {
		return false
	}
	return !m.joinedBroken(s, at, mapped)
}

func (m *sharedScope) bubbleJoined(s flow.Spawn, at ast.Node) bool {
	if !m.hasWaitCall || s.Body == nil || bodyParks(s.Body) {
		return false
	}
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool { return isSynctestWait(m.pass, n) })
}

func isSynctestWait(pass *analysis.Pass, n ast.Node) bool {
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
		if isWaitCall(pass.TypesInfo, call) || isWaitViaCall(pass, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func litWord(written bool) string {
	if written {
		return "written"
	}
	return "read"
}

func parentWord(written bool) string {
	if written {
		return "written"
	}
	return "read"
}
