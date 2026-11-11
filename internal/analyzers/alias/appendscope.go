package alias

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type appendScope struct {
	pass        *analysis.Pass
	fn          ast.Node
	info        *types.Info
	graph       *flow.Graph
	assigns     map[*types.Var][]ast.Node
	atPos       map[token.Pos]ast.Node
	copyDests   map[ast.Node]bool
	clearSlices map[ast.Node]bool
	plainStores map[*types.Var]map[*ast.Ident]bool
	clearArgs   map[*types.Var]map[*ast.Ident]bool
	indexReads  map[*types.Var][]*ast.IndexExpr
	sliceReads  map[*types.Var][]*ast.SliceExpr
	wholeReads  map[*types.Var][]*ast.Ident
	loops       []loopSpan
	wanted      map[*types.Var]bool
	untracked   map[*types.Var]bool
	ptrs        map[*types.Var]*types.Var
}

type loopSpan struct {
	node    ast.Node
	bodyPos token.Pos
	bodyEnd token.Pos
	size    token.Pos
}

func newAppendScope(pass *analysis.Pass, fn ast.Node, defs map[*types.Var]sliceDef) *appendScope {
	info := pass.TypesInfo
	s := &appendScope{
		pass:        pass,
		fn:          fn,
		info:        info,
		graph:       flow.NewGraph(pass, fn),
		assigns:     map[*types.Var][]ast.Node{},
		atPos:       map[token.Pos]ast.Node{},
		plainStores: map[*types.Var]map[*ast.Ident]bool{},
		clearArgs:   map[*types.Var]map[*ast.Ident]bool{},
	}
	seen := map[*types.Var]bool{}
	for _, d := range defs {
		seen[d.a] = true
	}
	s.wanted = seen
	s.ptrs = collectPtrAliases(info, fn, seen)
	collectAssignsAndLoops(s, info, fn)
	s.copyDests = copyDestNodes(info, fn)
	s.clearSlices = clearSliceNodes(info, fn)
	for a := range seen {
		s.plainStores[a] = plainStoreBases(s, a)
		s.clearArgs[a] = clearArgIdents(info, fn, a)
	}
	s.untracked = untrackedSlices(info, fn, seen, s.ptrs)
	s.indexReads = map[*types.Var][]*ast.IndexExpr{}
	s.sliceReads = map[*types.Var][]*ast.SliceExpr{}
	s.wholeReads = map[*types.Var][]*ast.Ident{}
	collectScopeReads(s, seen)
	return s
}

func collectAssignsAndLoops(s *appendScope, info *types.Info, fn ast.Node) {
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		collectLoopSpan(s, n)
		if st, ok := n.(*ast.AssignStmt); ok {
			s.atPos[st.Pos()] = st
			for _, lhs := range st.Lhs {
				recordAssignTarget(s, info, lhs, st)
			}
			return true
		}
		if t, ok := n.(*ast.RangeStmt); ok {
			recordRangeTarget(s, info, t)
		}
		return true
	})
}

func recordAssignTarget(s *appendScope, info *types.Info, lhs ast.Expr, st ast.Node) {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := varOf(info, id); ok {
		s.assigns[v] = append(s.assigns[v], st)
	}
}

func recordRangeTarget(s *appendScope, info *types.Info, t *ast.RangeStmt) {
	if t.Tok != token.ASSIGN {
		return
	}
	if id, ok := ast.Unparen(rangeAssignKey(t)).(*ast.Ident); ok {
		if v, ok := varOf(info, id); ok {
			s.assigns[v] = append(s.assigns[v], t)
		}
	}
	if id, ok := ast.Unparen(rangeAssignValue(t)).(*ast.Ident); ok {
		if v, ok := varOf(info, id); ok {
			s.assigns[v] = append(s.assigns[v], t)
		}
	}
}

func rangeAssignKey(t *ast.RangeStmt) ast.Expr {
	if t.Key == nil {
		return nil
	}
	return t.Key
}

func rangeAssignValue(t *ast.RangeStmt) ast.Expr {
	if t.Value == nil {
		return nil
	}
	return t.Value
}

func untrackedSlices(info *types.Info, fn ast.Node, wanted map[*types.Var]bool, ptrs map[*types.Var]*types.Var) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	markClosureTouch(info, fn, wanted, out)
	markFollowedStore(info, fn, ptrs, out)
	maps.DeleteFunc(ptrs, func(_ *types.Var, a *types.Var) bool { return out[a] })
	markAddrDefStore(info, fn, wanted, ptrs, out)
	return out
}

func markClosureTouch(info *types.Info, fn ast.Node, wanted, out map[*types.Var]bool) {
	for a := range wanted {
		if closureTouchesSlice(info, fn, a) {
			out[a] = true
		}
	}
}

func markFollowedStore(info *types.Info, fn ast.Node, ptrs map[*types.Var]*types.Var, out map[*types.Var]bool) {
	for p, a := range ptrs {
		if !ptrReadOnly(info, fn, p) {
			out[a] = true
		}
	}
}

func markAddrDefStore(info *types.Info, fn ast.Node, wanted map[*types.Var]bool, ptrs map[*types.Var]*types.Var, out map[*types.Var]bool) {
	for p, as := range ptrAddrDefs(info, fn, wanted) {
		if _, ok := ptrs[p]; ok {
			continue
		}
		if addrDefClean(info, fn, p) {
			continue
		}
		for a := range as {
			out[a] = true
		}
	}
}

func addrDefClean(info *types.Info, fn ast.Node, p *types.Var) bool {
	return ptrReadOnly(info, fn, p) && !ptrElementStored(info, fn, p)
}

func addrTakenReachesAppend(s *appendScope, a *types.Var, st *ast.AssignStmt) bool {
	body := flow.Body(s.fn)
	if body == nil || s.graph == nil {
		return false
	}
	found := false
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if found {
			return false
		}
		if isAddrReachesNode(s, stack, n, a, st) {
			found = true
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		stack = append(stack, n)
		return true
	})
	return found
}

func isAddrReachesNode(s *appendScope, stack []ast.Node, n ast.Node, a *types.Var, st *ast.AssignStmt) bool {
	u, ok := n.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND || !identIsVar(s.info, u.X, a) {
		return false
	}
	return addrDisallowedReaches(s, stack, u, a, st)
}

func addrDisallowedReaches(s *appendScope, stack []ast.Node, u *ast.UnaryExpr, a *types.Var, st *ast.AssignStmt) bool {
	if addrAllowedFollowed(s, stack, u, a) {
		return false
	}
	return s.graph.Reaches(u, st, nil)
}

func addrAllowedFollowed(s *appendScope, stack []ast.Node, u *ast.UnaryExpr, a *types.Var) bool {
	if len(stack) == 0 {
		return false
	}
	top := stack[len(stack)-1]
	if st, ok := top.(*ast.AssignStmt); ok {
		return assignAddrFollowed(s, st, u, a)
	}
	if spec, ok := top.(*ast.ValueSpec); ok {
		return valueAddrFollowed(s, spec, u, a)
	}
	return false
}

func assignAddrFollowed(s *appendScope, st *ast.AssignStmt, u *ast.UnaryExpr, a *types.Var) bool {
	p, ok := assignLHSPointer(s, st, u)
	if !ok {
		return false
	}
	if p == nil {
		return true
	}
	if q, ok := s.ptrs[p]; ok && q == a && st.Tok == token.DEFINE {
		return true
	}
	return isBodyLocal(s.fn, p) && addrDefClean(s.info, s.fn, p)
}

func assignLHSPointer(s *appendScope, st *ast.AssignStmt, u *ast.UnaryExpr) (*types.Var, bool) {
	if !assignShapeMatches(st, u) {
		return nil, false
	}
	if isBlankAssign(st) {
		return nil, true
	}
	id, ok := ast.Unparen(st.Lhs[0]).(*ast.Ident)
	if !ok {
		return nil, false
	}
	return varOf(s.info, id)
}

func assignShapeMatches(st *ast.AssignStmt, u *ast.UnaryExpr) bool {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return false
	}
	return ast.Unparen(st.Rhs[0]) == ast.Node(u)
}

func isBlankAssign(st *ast.AssignStmt) bool {
	id, ok := ast.Unparen(st.Lhs[0]).(*ast.Ident)
	return ok && id.Name == "_"
}

func valueAddrFollowed(s *appendScope, spec *ast.ValueSpec, u *ast.UnaryExpr, a *types.Var) bool {
	if len(spec.Names) != 1 || len(spec.Values) != 1 {
		return false
	}
	if ast.Unparen(spec.Values[0]) != ast.Node(u) {
		return false
	}
	p, ok := varOf(s.info, spec.Names[0])
	if !ok {
		return false
	}
	if q, ok := s.ptrs[p]; ok && q == a {
		return true
	}
	return isBodyLocal(s.fn, p) && addrDefClean(s.info, s.fn, p)
}

func closureTouchesSlice(info *types.Info, fn ast.Node, a *types.Var) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	touched := false
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if touched {
			return false
		}
		if insideLiteral(stack) && assignsOrTakesAddr(info, n, a) {
			touched = true
			return false
		}
		stack = append(stack, n)
		return true
	})
	return touched
}

func insideLiteral(stack []ast.Node) bool {
	for _, an := range stack {
		if _, ok := an.(*ast.FuncLit); ok {
			return true
		}
	}
	return false
}

func assignsOrTakesAddr(info *types.Info, n ast.Node, a *types.Var) bool {
	if st, ok := n.(*ast.AssignStmt); ok {
		return assignWritesVar(info, st, a)
	}
	if u, ok := n.(*ast.UnaryExpr); ok {
		return u.Op == token.AND && identIsVar(info, u.X, a)
	}
	return false
}

func assignWritesVar(info *types.Info, st *ast.AssignStmt, a *types.Var) bool {
	for _, lhs := range st.Lhs {
		if id, ok := ast.Unparen(lhs).(*ast.Ident); ok && identIsVar(info, id, a) {
			return true
		}
	}
	return false
}

func identIsVar(info *types.Info, e ast.Expr, a *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := varOf(info, id)
	return ok && v == a
}

func collectLoopSpan(s *appendScope, n ast.Node) {
	switch t := n.(type) {
	case *ast.ForStmt:
		if t.Body != nil {
			s.loops = append(s.loops, loopSpan{node: n, bodyPos: t.Body.Pos(), bodyEnd: t.Body.End(), size: t.Body.End() - t.Body.Pos()})
		}
	case *ast.RangeStmt:
		if t.Body != nil {
			s.loops = append(s.loops, loopSpan{node: n, bodyPos: t.Body.Pos(), bodyEnd: t.Body.End(), size: t.Body.End() - t.Body.Pos()})
		}
	}
}

func collectScopeReads(s *appendScope, wanted map[*types.Var]bool) {
	var stack []ast.Node
	ast.Inspect(flow.Body(s.fn), func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch t := n.(type) {
		case *ast.IndexExpr:
			collectScopeIndex(s, wanted, t)
		case *ast.SliceExpr:
			if !isDirectLenCapArg(s, stack, t) {
				collectScopeSlice(s, wanted, t)
			}
		default:
			collectScopeWhole(s, wanted, n)
		}
		stack = append(stack, n)
		return true
	})
}

func isDirectLenCapArg(s *appendScope, stack []ast.Node, t *ast.SliceExpr) bool {
	if len(stack) == 0 {
		return false
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok || !isLenCapCall(s.info, call) {
		return false
	}
	for _, arg := range call.Args {
		if ast.Unparen(arg) == ast.Node(t) {
			return true
		}
	}
	return false
}

func collectScopeIndex(s *appendScope, wanted map[*types.Var]bool, t *ast.IndexExpr) {
	v, id, ok := resolveIndexBase(s.info, wanted, s.ptrs, t.X)
	if !ok || s.plainStores[v][id] {
		return
	}
	s.indexReads[v] = append(s.indexReads[v], t)
}

func collectScopeSlice(s *appendScope, wanted map[*types.Var]bool, t *ast.SliceExpr) {
	v, id, ok := resolveIndexBase(s.info, wanted, s.ptrs, t.X)
	if !ok || s.clearArgs[v][id] {
		return
	}
	if s.copyDests[ast.Unparen(t)] || s.clearSlices[ast.Unparen(t)] {
		return
	}
	s.sliceReads[v] = append(s.sliceReads[v], t)
}

func collectScopeWhole(s *appendScope, wanted map[*types.Var]bool, n ast.Node) {
	switch t := n.(type) {
	case *ast.CallExpr:
		collectScopeCall(s, wanted, t)
	case *ast.ReturnStmt:
		collectScopeReturn(s, wanted, t)
	case *ast.RangeStmt:
		collectScopeRange(s, wanted, t)
	case *ast.AssignStmt:
		collectScopeAssign(s, wanted, t)
	case *ast.SendStmt:
		collectScopeSend(s, wanted, t)
	case *ast.CompositeLit:
		collectScopeComposite(s, wanted, t)
	case *ast.ValueSpec:
		collectScopeValue(s, wanted, t)
	}
}

func collectScopeCall(s *appendScope, wanted map[*types.Var]bool, call *ast.CallExpr) {
	if isLenCapCall(s.info, call) || isClearCall(s.info, call) {
		return
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if v, id, ok := scopeWholeIdent(s, wanted, sel.X); ok {
			s.wholeReads[v] = append(s.wholeReads[v], id)
		}
	}
	skip := isCopyCall(s.info, call)
	for i, arg := range call.Args {
		if skip && i == 0 {
			continue
		}
		if v, id, ok := scopeWholeIdent(s, wanted, arg); ok {
			s.wholeReads[v] = append(s.wholeReads[v], id)
		}
	}
}

func collectScopeAssign(s *appendScope, wanted map[*types.Var]bool, st *ast.AssignStmt) {
	if !scopeStoredOutside(s, st.Lhs) {
		return
	}
	for _, rhs := range st.Rhs {
		if v, id, ok := scopeWholeIdent(s, wanted, rhs); ok {
			s.wholeReads[v] = append(s.wholeReads[v], id)
		}
	}
}

func collectScopeValue(s *appendScope, wanted map[*types.Var]bool, spec *ast.ValueSpec) {
	if !scopeNamesOutside(s, spec.Names) {
		return
	}
	for _, val := range spec.Values {
		if v, id, ok := scopeWholeIdent(s, wanted, val); ok {
			s.wholeReads[v] = append(s.wholeReads[v], id)
		}
	}
}

func collectScopeReturn(s *appendScope, wanted map[*types.Var]bool, t *ast.ReturnStmt) {
	for _, res := range t.Results {
		if v, id, ok := scopeWholeIdent(s, wanted, res); ok {
			s.wholeReads[v] = append(s.wholeReads[v], id)
		}
	}
}

func collectScopeRange(s *appendScope, wanted map[*types.Var]bool, t *ast.RangeStmt) {
	if t.Value == nil {
		return
	}
	if id, ok := ast.Unparen(t.Value).(*ast.Ident); ok && id.Name == "_" {
		return
	}
	if v, id, ok := scopeWholeIdent(s, wanted, t.X); ok {
		s.wholeReads[v] = append(s.wholeReads[v], id)
	}
}

func collectScopeSend(s *appendScope, wanted map[*types.Var]bool, t *ast.SendStmt) {
	if v, id, ok := scopeWholeIdent(s, wanted, t.Value); ok {
		s.wholeReads[v] = append(s.wholeReads[v], id)
	}
}

func collectScopeComposite(s *appendScope, wanted map[*types.Var]bool, t *ast.CompositeLit) {
	for _, elt := range t.Elts {
		if v, id, ok := scopeWholeIdent(s, wanted, elt); ok {
			s.wholeReads[v] = append(s.wholeReads[v], id)
			continue
		}
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if v, id, ok := scopeWholeIdent(s, wanted, kv.Value); ok {
				s.wholeReads[v] = append(s.wholeReads[v], id)
			}
		}
	}
}

func scopeWholeIdent(s *appendScope, wanted map[*types.Var]bool, e ast.Expr) (*types.Var, *ast.Ident, bool) {
	e = ast.Unparen(e)
	if star, ok := e.(*ast.StarExpr); ok {
		return starWhole(s, wanted, star)
	}
	return plainWhole(s, wanted, e)
}

func starWhole(s *appendScope, wanted map[*types.Var]bool, star *ast.StarExpr) (*types.Var, *ast.Ident, bool) {
	inner, ok := ast.Unparen(star.X).(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	p, ok := varOf(s.info, inner)
	if !ok {
		return nil, nil, false
	}
	a, ok := s.ptrs[p]
	if !ok || !wanted[a] {
		return nil, nil, false
	}
	return a, inner, true
}

func plainWhole(s *appendScope, wanted map[*types.Var]bool, e ast.Expr) (*types.Var, *ast.Ident, bool) {
	if u, ok := e.(*ast.UnaryExpr); ok {
		if u.Op != token.AND {
			return nil, nil, false
		}
		e = ast.Unparen(u.X)
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	v, ok := varOf(s.info, id)
	if !ok {
		return nil, nil, false
	}
	if wanted[v] {
		return v, id, true
	}
	if a, ok := s.ptrs[v]; ok && wanted[a] {
		return a, id, true
	}
	return nil, nil, false
}

func scopeStoredOutside(s *appendScope, lhs []ast.Expr) bool {
	for _, e := range lhs {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return true
		}
		if id.Name == "_" {
			continue
		}
		v, ok := varOf(s.info, id)
		if !ok || !scopeLocalAlias(s, v) {
			return true
		}
	}
	return false
}

func scopeNamesOutside(s *appendScope, names []*ast.Ident) bool {
	for _, id := range names {
		if id.Name == "_" {
			continue
		}
		v, ok := varOf(s.info, id)
		if !ok || !scopeLocalAlias(s, v) {
			return true
		}
	}
	return false
}

func scopeLocalAlias(s *appendScope, v *types.Var) bool {
	if v == nil {
		return false
	}
	if isParam(s.info, s.fn, v) {
		return true
	}
	body := flow.Body(s.fn)
	if body == nil || !v.Pos().IsValid() {
		return false
	}
	return body.Pos() <= v.Pos() && v.Pos() < body.End()
}

func isParam(info *types.Info, fn ast.Node, v *types.Var) bool {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		if definesVar(info, fn.Recv, v) {
			return true
		}
		if fn.Type == nil {
			return false
		}
		return definesVar(info, fn.Type.Params, v)
	case *ast.FuncLit:
		if fn.Type == nil {
			return false
		}
		return definesVar(info, fn.Type.Params, v)
	}
	return false
}

func definesVar(info *types.Info, list *ast.FieldList, v *types.Var) bool {
	if list == nil {
		return false
	}
	for _, field := range list.List {
		for _, id := range field.Names {
			if info.Defs[id] == v {
				return true
			}
		}
	}
	return false
}

func enclosingLoopCached(s *appendScope, target *ast.AssignStmt) ast.Node {
	var best ast.Node
	bestSize := token.Pos(1 << 30)
	for _, l := range s.loops {
		if target.Pos() <= l.bodyPos || target.End() > l.bodyEnd {
			continue
		}
		if l.size < bestSize {
			bestSize = l.size
			best = l.node
		}
	}
	return best
}

func clobberEnd(scope *appendScope, st *ast.AssignStmt, def sliceDef, appended int) int {
	loop := enclosingLoopCached(scope, st)
	if loop == nil {
		return def.high + appended
	}
	if rangeOverSlice(scope.info, loop, def.a) {
		return 1 << 30
	}
	if forCondLenOf(scope.info, loop, def.a) {
		return 1 << 30
	}
	return def.high + appended
}

func rangeOverSlice(info *types.Info, loop ast.Node, a *types.Var) bool {
	t, ok := loop.(*ast.RangeStmt)
	if !ok || t.X == nil {
		return false
	}
	v, ok := identVar(info, t.X)
	return ok && v == a
}

func forCondLenOf(info *types.Info, loop ast.Node, a *types.Var) bool {
	t, ok := loop.(*ast.ForStmt)
	if !ok || t.Cond == nil {
		return false
	}
	return condMentionsLen(info, t.Cond, a)
}

func condMentionsLen(info *types.Info, cond ast.Expr, a *types.Var) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if found {
			return false
		}
		if e, ok := n.(ast.Expr); ok && isLenOf(info, e, a) {
			found = true
			return false
		}
		return true
	})
	return found
}
