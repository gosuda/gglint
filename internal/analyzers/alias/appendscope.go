package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type appendScope struct {
	pass        *analysis.Pass
	fn          ast.Node
	info        *types.Info
	graph       *flow.Graph
	assigns     map[*types.Var][]*ast.AssignStmt
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
		assigns:     map[*types.Var][]*ast.AssignStmt{},
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
		s.plainStores[a] = plainStoreBases(info, fn, a, s.ptrs)
		s.clearArgs[a] = clearArgIdents(info, fn, a, s.ptrs)
	}
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
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		s.atPos[st.Pos()] = st
		for _, lhs := range st.Lhs {
			recordAssignTarget(s, info, lhs, st)
		}
		return true
	})
}

func recordAssignTarget(s *appendScope, info *types.Info, lhs ast.Expr, st *ast.AssignStmt) {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := varOf(info, id); ok {
			s.assigns[v] = append(s.assigns[v], st)
		}
		return
	}
	star, ok := ast.Unparen(lhs).(*ast.StarExpr)
	if !ok {
		return
	}
	inner, ok := ast.Unparen(star.X).(*ast.Ident)
	if !ok {
		return
	}
	p, ok := varOf(info, inner)
	if !ok {
		return
	}
	if a, ok := s.ptrs[p]; ok && s.wanted[a] {
		s.assigns[a] = append(s.assigns[a], st)
	}
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
	ast.Inspect(flow.Body(s.fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch t := n.(type) {
		case *ast.IndexExpr:
			collectScopeIndex(s, wanted, t)
		case *ast.SliceExpr:
			collectScopeSlice(s, wanted, t)
		default:
			collectScopeWhole(s, wanted, n)
		}
		return true
	})
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

func clobberEnd(scope *appendScope, st *ast.AssignStmt, bound, appended int) int {
	if enclosingLoopCached(scope, st) != nil {
		return 1 << 30
	}
	return bound + appended
}
