package gorace

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

type varAccess struct {
	key   flow.Key
	stmt  ast.Stmt
	write bool
	index bool
}

type mapScope struct {
	pass   *analysis.Pass
	graph  *flow.Graph
	fn     *ast.FuncDecl
	parent []varAccess
	lits   map[*ast.FuncLit][]varAccess
}

type sharedScope struct {
	pass   *analysis.Pass
	graph  *flow.Graph
	fn     *ast.FuncDecl
	parent []varAccess
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
			out = append(out, varAccess{key: k, stmt: stmt, write: true, index: true})
		}
		return out
	}
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := info.Defs[id].(*types.Var); ok {
			return append(out, varAccess{key: flow.Key{Root: v}, stmt: stmt, write: true})
		}
	}
	if k, ok := flow.KeyOf(info, lhs); ok {
		out = append(out, varAccess{key: k, stmt: stmt, write: true})
	}
	return out
}

func appendIncWrites(info *types.Info, n *ast.IncDecStmt, stmt ast.Stmt, out []varAccess) []varAccess {
	if idx, ok := ast.Unparen(n.X).(*ast.IndexExpr); ok {
		if k, ok := flow.KeyOf(info, idx.X); ok {
			out = append(out, varAccess{key: k, stmt: stmt, write: true, index: true})
		}
		return out
	}
	if k, ok := flow.KeyOf(info, n.X); ok {
		out = append(out, varAccess{key: k, stmt: stmt, write: true})
	}
	return out
}

func appendDeleteWrite(info *types.Info, n *ast.CallExpr, stmt ast.Stmt, out []varAccess) []varAccess {
	id, ok := ast.Unparen(n.Fun).(*ast.Ident)
	if !ok || len(n.Args) == 0 || !isBuiltin(info, id, "delete") {
		return out
	}
	if k, ok := flow.KeyOf(info, n.Args[0]); ok {
		out = append(out, varAccess{key: k, stmt: stmt, write: true})
	}
	return out
}

func isBuiltin(info *types.Info, id *ast.Ident, name string) bool {
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
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

func appendReadNode(info *types.Info, n ast.Node, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) (bool, bool) {
	if _, ok := n.(*ast.FuncLit); ok {
		return true, false
	}
	if call, ok := n.(*ast.CallExpr); ok && isLenCall(info, call) {
		return true, false
	}
	if sel, ok := n.(*ast.SelectorExpr); ok && info.Selections[sel] != nil {
		if k, ok := flow.KeyOf(info, sel); ok && !skip[k] {
			*out = append(*out, varAccess{key: k, stmt: stmt})
		}
		return true, false
	}
	if id, ok := n.(*ast.Ident); ok {
		appendIdentRead(info, id, stmt, skip, out)
	}
	return false, true
}

func appendIdentRead(info *types.Info, id *ast.Ident, stmt ast.Stmt, skip map[flow.Key]bool, out *[]varAccess) {
	v, ok := info.Uses[id].(*types.Var)
	if !ok || id.Name == "_" || skip[flow.Key{Root: v}] {
		return
	}
	*out = append(*out, varAccess{key: flow.Key{Root: v}, stmt: stmt})
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

func collectParent(info *types.Info, fn *ast.FuncDecl, sites map[ast.Node]bool) []varAccess {
	var out []varAccess
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if gs, ok := n.(*ast.GoStmt); ok {
			_ = gs
			return false
		}
		out = appendParentNode(info, n, sites, out)
		return true
	})
	return out
}

func appendParentNode(info *types.Info, n ast.Node, sites map[ast.Node]bool, out []varAccess) []varAccess {
	switch n := n.(type) {
	case *ast.AssignStmt, *ast.IncDecStmt, *ast.ReturnStmt:
		return append(out, stmtAccesses(info, n.(ast.Stmt))...)
	case *ast.ExprStmt:
		return appendParentExpr(info, n, sites, out)
	case *ast.IfStmt:
		return append(out, condAccesses(info, n, n.Cond)...)
	case *ast.ForStmt:
		return append(out, condAccesses(info, n, n.Cond)...)
	case *ast.RangeStmt:
		return append(out, condAccesses(info, n, n.X)...)
	}
	return out
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

func appendCondNode(info *types.Info, n ast.Node, stmt ast.Stmt, out *[]varAccess) (bool, bool) {
	if _, ok := n.(*ast.FuncLit); ok {
		return true, false
	}
	if call, ok := n.(*ast.CallExpr); ok && isLenCall(info, call) {
		return true, false
	}
	if sel, ok := n.(*ast.SelectorExpr); ok && info.Selections[sel] != nil {
		if k, ok := flow.KeyOf(info, sel); ok {
			*out = append(*out, varAccess{key: k, stmt: stmt})
		}
		return true, false
	}
	if id, ok := n.(*ast.Ident); ok {
		if v, ok := info.Uses[id].(*types.Var); ok && id.Name != "_" {
			*out = append(*out, varAccess{key: flow.Key{Root: v}, stmt: stmt})
		}
	}
	return false, true
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
	switch n := n.(type) {
	case *ast.AssignStmt, *ast.IncDecStmt, *ast.ExprStmt, *ast.ReturnStmt:
		return append(out, stmtAccesses(info, n.(ast.Stmt))...)
	}
	return out
}

func runMap(pass *analysis.Pass) (any, error) {
	for _, cur := range funcDecls(pass) {
		checkMap(pass, inspection.As[*ast.FuncDecl](cur.Node()))
	}
	return nil, nil
}

func checkMap(pass *analysis.Pass, fn *ast.FuncDecl) {
	info := pass.TypesInfo
	if passesMutex(info, fn) {
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
	scope := &mapScope{pass: pass, graph: g, fn: fn, parent: collectParent(info, fn, sites), lits: map[*ast.FuncLit][]varAccess{}}
	for _, s := range spawns {
		if s.Body != nil {
			scope.lits[s.Body] = collectLit(info, s.Body)
		}
	}
	for _, s := range spawns {
		if s.Body == nil {
			continue
		}
		scope.checkSpawn(s)
	}
}

func (m *mapScope) checkSpawn(s flow.Spawn) {
	for _, a := range m.lits[s.Body] {
		if !mapReportable(m.pass, s, a) || mapGuarded(m.pass, s, a) {
			continue
		}
		if s.InLoop || m.hasOther(s, a.key) {
			m.pass.Reportf(s.Site.Pos(), "%s is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex", keyText(a.key))
			return
		}
	}
}

func mapGuarded(pass *analysis.Pass, s flow.Spawn, a varAccess) bool {
	return passesMutex(pass.TypesInfo, s.Body) || len(heldOutside(pass, s.Body, a.stmt)) > 0
}

func mapReportable(pass *analysis.Pass, s flow.Spawn, a varAccess) bool {
	if !a.write {
		return false
	}
	v, ok := a.key.Root.(*types.Var)
	if !ok || !outsideLit(v, s.Body) || isSyncType(v.Type()) || !isMapType(v.Type()) {
		return false
	}
	return true
}

func (m *mapScope) hasOther(s flow.Spawn, k flow.Key) bool {
	for _, p := range m.parent {
		if m.parentConflicts(s, k, p) {
			return true
		}
	}
	return m.otherSpawnWrites(s, k)
}

func (m *mapScope) parentConflicts(s flow.Spawn, k flow.Key, p varAccess) bool {
	if p.key != k || !m.graph.Reaches(s.Site, p.stmt, nil) || flow.Joined(m.pass, m.graph, s, p.stmt) {
		return false
	}
	return !commonKey(heldOutside(m.pass, s.Body, writeStmtFor(m.lits[s.Body], k)), parentHeld(m.pass, m.graph, p.stmt))
}

func (m *mapScope) otherSpawnWrites(s flow.Spawn, k flow.Key) bool {
	for _, o := range flow.Spawns(m.pass, m.fn) {
		if o.Body == nil || o.Body == s.Body {
			continue
		}
		for _, a := range m.lits[o.Body] {
			if a.key == k {
				return true
			}
		}
	}
	return false
}

func writeStmtFor(acc []varAccess, k flow.Key) ast.Node {
	for _, a := range acc {
		if a.key == k {
			return a.stmt
		}
	}
	return nil
}

func runFanout(pass *analysis.Pass) (any, error) {
	for _, cur := range funcDecls(pass) {
		checkFanout(pass, inspection.As[*ast.FuncDecl](cur.Node()))
	}
	return nil, nil
}

func checkFanout(pass *analysis.Pass, fn *ast.FuncDecl) {
	info := pass.TypesInfo
	if passesMutex(info, fn) {
		return
	}
	for _, s := range flow.Spawns(pass, fn) {
		if !s.InLoop || s.Body == nil || passesMutex(info, s.Body) {
			continue
		}
		for _, a := range collectLit(info, s.Body) {
			reportFanoutWrite(pass, fn, s, a)
		}
	}
}

func reportFanoutWrite(pass *analysis.Pass, fn *ast.FuncDecl, s flow.Spawn, a varAccess) {
	if !fanoutReportable(pass, fn, s, a) {
		return
	}
	pass.Reportf(a.stmt.Pos(), "%s is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex", keyText(a.key))
}

func fanoutReportable(pass *analysis.Pass, fn *ast.FuncDecl, s flow.Spawn, a varAccess) bool {
	if !a.write || a.index {
		return false
	}
	v, ok := a.key.Root.(*types.Var)
	if !ok || !outsideLit(v, s.Body) || isSyncType(v.Type()) || isMapType(v.Type()) || !declaredOutsideLoop(fn, v, s.Site) {
		return false
	}
	if !flow.IsLocal(pass.TypesInfo, fn, v) && !isPackageVar(pass, v) {
		return false
	}
	return len(heldOutside(pass, s.Body, a.stmt)) == 0
}

func isPackageVar(pass *analysis.Pass, v *types.Var) bool {
	if pass.Pkg == nil || pass.Pkg.Scope() == nil {
		return false
	}
	return pass.Pkg.Scope().Lookup(v.Name()) == v
}

func runShared(pass *analysis.Pass) (any, error) {
	for _, cur := range funcDecls(pass) {
		checkShared(pass, inspection.As[*ast.FuncDecl](cur.Node()))
	}
	return nil, nil
}

func checkShared(pass *analysis.Pass, fn *ast.FuncDecl) {
	info := pass.TypesInfo
	if passesMutex(info, fn) {
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
	scope := &sharedScope{pass: pass, graph: g, fn: fn, parent: collectParent(info, fn, sites)}
	for _, s := range spawns {
		if s.InLoop || s.Body == nil {
			continue
		}
		scope.checkSpawn(s)
	}
}

func (m *sharedScope) checkSpawn(s flow.Spawn) {
	lit := collectLit(m.pass.TypesInfo, s.Body)
	for _, k := range sharedKeys(m.pass, m.fn, s, lit) {
		if m.conflict(s, k, lit) {
			return
		}
	}
}

func sharedKeys(pass *analysis.Pass, fn *ast.FuncDecl, s flow.Spawn, lit []varAccess) []flow.Key {
	seen := map[flow.Key]bool{}
	var order []flow.Key
	for _, a := range lit {
		v, ok := a.key.Root.(*types.Var)
		if !ok || !outsideLit(v, s.Body) || isSyncType(v.Type()) || isMapType(v.Type()) || !flow.IsLocal(pass.TypesInfo, fn, v) {
			continue
		}
		if !seen[a.key] {
			seen[a.key] = true
			order = append(order, a.key)
		}
	}
	return order
}

func (m *sharedScope) conflict(s flow.Spawn, k flow.Key, lit []varAccess) bool {
	first := earliestParent(m.graph, s, k, litHasWrite(lit, k), m.parent)
	if first == nil || flow.Joined(m.pass, m.graph, s, first.stmt) || passesMutex(m.pass.TypesInfo, s.Body) {
		return false
	}
	if stmt := writeStmtFor(lit, k); stmt != nil && commonKey(heldOutside(m.pass, s.Body, stmt), parentHeld(m.pass, m.graph, first.stmt)) {
		return false
	}
	line := m.pass.Fset.Position(first.stmt.Pos()).Line
	m.pass.Reportf(s.Site.Pos(), "%s is %s by this goroutine and %s at line %d before the goroutine is joined: pass it as an argument, or wait for the goroutine first", keyText(k), litWord(litHasWrite(lit, k)), parentWord(first.write), line)
	return true
}

func earliestParent(g *flow.Graph, s flow.Spawn, k flow.Key, litWrite bool, parent []varAccess) *varAccess {
	var first *varAccess
	for i := range parent {
		p := parent[i]
		if p.key != k {
			continue
		}
		if !litWrite && !p.write {
			continue
		}
		if !g.Reaches(s.Site, p.stmt, nil) {
			continue
		}
		if first == nil || p.stmt.Pos() < first.stmt.Pos() {
			cp := p
			first = &cp
		}
	}
	return first
}

func litHasWrite(lit []varAccess, k flow.Key) bool {
	for _, a := range lit {
		if a.key == k && a.write {
			return true
		}
	}
	return false
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
