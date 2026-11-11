package alias

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/gosuda/gglint/internal/flow"
)

type ptrScan struct {
	info   *types.Info
	wanted map[*types.Var]bool
	counts map[*types.Var]int
	target map[*types.Var]*types.Var
	bad    map[*types.Var]bool
	taken  map[*types.Var]bool
}

func collectPtrAliases(info *types.Info, fn ast.Node, wanted map[*types.Var]bool) map[*types.Var]*types.Var {
	s := &ptrScan{
		info:   info,
		wanted: wanted,
		counts: map[*types.Var]int{},
		target: map[*types.Var]*types.Var{},
		bad:    map[*types.Var]bool{},
		taken:  map[*types.Var]bool{},
	}
	ast.Inspect(flow.Body(fn), s.visit)
	return s.result()
}

func (s *ptrScan) visit(n ast.Node) bool {
	switch t := n.(type) {
	case *ast.AssignStmt:
		s.scanAssign(t)
	case *ast.ValueSpec:
		s.scanValue(t)
	case *ast.RangeStmt:
		s.scanRange(t)
	case *ast.IncDecStmt:
		s.scanIncDec(t)
	case *ast.UnaryExpr:
		s.scanAddr(t)
	}
	return true
}

func (s *ptrScan) scanAssign(st *ast.AssignStmt) {
	for _, lhs := range st.Lhs {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := varOf(s.info, id)
		if !ok {
			continue
		}
		s.counts[v]++
		if s.isAddrDefine(st, id, v) {
			s.noteTarget(v, st.Rhs[0])
		} else {
			s.bad[v] = true
		}
	}
}

func (s *ptrScan) isAddrDefine(st *ast.AssignStmt, id *ast.Ident, v *types.Var) bool {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return false
	}
	if st.Tok != token.DEFINE {
		return false
	}
	return s.info.Defs[id] == v
}

func (s *ptrScan) noteTarget(v *types.Var, rhs ast.Expr) {
	a, ok := addrTarget(s.info, rhs, s.wanted)
	if !ok {
		s.bad[v] = true
		return
	}
	s.target[v] = a
}

func (s *ptrScan) scanValue(spec *ast.ValueSpec) {
	for _, name := range spec.Names {
		v, ok := varOf(s.info, name)
		if !ok {
			continue
		}
		s.counts[v]++
		if len(spec.Values) != 1 || len(spec.Names) != 1 {
			s.bad[v] = true
			continue
		}
		s.noteTarget(v, spec.Values[0])
	}
}

func (s *ptrScan) scanRange(t *ast.RangeStmt) {
	s.markDef(t.Key)
	s.markDef(t.Value)
}

func (s *ptrScan) markDef(e ast.Expr) {
	if e == nil {
		return
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || id.Name == "_" {
		return
	}
	if v, ok := varOf(s.info, id); ok {
		s.counts[v]++
		s.bad[v] = true
	}
}

func (s *ptrScan) scanIncDec(t *ast.IncDecStmt) {
	id, ok := ast.Unparen(t.X).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := varOf(s.info, id); ok {
		s.counts[v]++
		s.bad[v] = true
	}
}

func (s *ptrScan) scanAddr(t *ast.UnaryExpr) {
	if t.Op != token.AND {
		return
	}
	id, ok := ast.Unparen(t.X).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := varOf(s.info, id); ok {
		s.taken[v] = true
	}
}

func (s *ptrScan) result() map[*types.Var]*types.Var {
	out := map[*types.Var]*types.Var{}
	for v, c := range s.counts {
		if c != 1 || s.bad[v] || s.taken[v] {
			continue
		}
		a, ok := s.target[v]
		if !ok || !s.wanted[a] {
			continue
		}
		out[v] = a
	}
	return out
}

func addrTarget(info *types.Info, e ast.Expr, wanted map[*types.Var]bool) (*types.Var, bool) {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil, false
	}
	id, ok := ast.Unparen(u.X).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := varOf(info, id)
	if !ok || !wanted[v] {
		return nil, false
	}
	return v, true
}

func resolveIndexBase(info *types.Info, wanted map[*types.Var]bool, ptrs map[*types.Var]*types.Var, x ast.Expr) (*types.Var, *ast.Ident, bool) {
	ux := ast.Unparen(x)
	if id, ok := ux.(*ast.Ident); ok {
		v, ok := varOf(info, id)
		if ok && wanted[v] {
			return v, id, true
		}
		return nil, nil, false
	}
	star, ok := ux.(*ast.StarExpr)
	if !ok {
		return nil, nil, false
	}
	inner, ok := ast.Unparen(star.X).(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	p, ok := varOf(info, inner)
	if !ok {
		return nil, nil, false
	}
	a, ok := ptrs[p]
	if !ok || !wanted[a] {
		return nil, nil, false
	}
	return a, inner, true
}

func ptrAddrDefs(info *types.Info, fn ast.Node, wanted map[*types.Var]bool) map[*types.Var]map[*types.Var]bool {
	out := map[*types.Var]map[*types.Var]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		recordAddrDef(info, n, wanted, out)
		return true
	})
	return out
}

func recordAddrDef(info *types.Info, n ast.Node, wanted map[*types.Var]bool, out map[*types.Var]map[*types.Var]bool) {
	switch t := n.(type) {
	case *ast.AssignStmt:
		recordAssignAddrDef(info, t, wanted, out)
	case *ast.ValueSpec:
		recordValueAddrDef(info, t, wanted, out)
	}
}

func recordAssignAddrDef(info *types.Info, st *ast.AssignStmt, wanted map[*types.Var]bool, out map[*types.Var]map[*types.Var]bool) {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return
	}
	id, ok := ast.Unparen(st.Lhs[0]).(*ast.Ident)
	if !ok {
		return
	}
	p, ok := varOf(info, id)
	if !ok {
		return
	}
	if a, ok := addrTarget(info, st.Rhs[0], wanted); ok {
		addAddrDef(out, p, a)
	}
}

func recordValueAddrDef(info *types.Info, spec *ast.ValueSpec, wanted map[*types.Var]bool, out map[*types.Var]map[*types.Var]bool) {
	if len(spec.Names) != 1 || len(spec.Values) != 1 {
		return
	}
	p, ok := varOf(info, spec.Names[0])
	if !ok {
		return
	}
	if a, ok := addrTarget(info, spec.Values[0], wanted); ok {
		addAddrDef(out, p, a)
	}
}

func addAddrDef(out map[*types.Var]map[*types.Var]bool, p, a *types.Var) {
	m, ok := out[p]
	if !ok {
		m = map[*types.Var]bool{}
		out[p] = m
	}
	m[a] = true
}

func ptrReadOnly(info *types.Info, fn ast.Node, p *types.Var) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	clean := true
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if !clean {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := varOf(info, id); ok && v == p && !ptrUseClean(info, id, stack) {
				clean = false
				return false
			}
		}
		stack = append(stack, n)
		return true
	})
	return clean
}

func ptrUseClean(info *types.Info, id *ast.Ident, stack []ast.Node) bool {
	for _, an := range stack {
		if _, ok := an.(*ast.FuncLit); ok {
			return false
		}
	}
	if isPtrDefIdent(info, id, stack) {
		return true
	}
	if isReassignTarget(id, stack) {
		return true
	}
	return isReadThrough(stack)
}

func isReassignTarget(id *ast.Ident, stack []ast.Node) bool {
	idx := len(stack) - 1
	for idx >= 0 {
		if _, ok := stack[idx].(*ast.ParenExpr); ok {
			idx--
			continue
		}
		break
	}
	if idx < 0 {
		return false
	}
	st, ok := stack[idx].(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, lhs := range st.Lhs {
		if ast.Unparen(lhs) == ast.Node(id) {
			return true
		}
	}
	return false
}

func isPtrDefIdent(info *types.Info, id *ast.Ident, stack []ast.Node) bool {
	if len(stack) == 0 || info.Defs[id] == nil {
		return false
	}
	if st, ok := stack[len(stack)-1].(*ast.AssignStmt); ok {
		return isShortDefine(st, id)
	}
	if spec, ok := stack[len(stack)-1].(*ast.ValueSpec); ok {
		return valueNames(spec, id)
	}
	return false
}

func isShortDefine(st *ast.AssignStmt, id *ast.Ident) bool {
	return st.Tok == token.DEFINE && len(st.Lhs) == 1 && len(st.Rhs) == 1 && ast.Unparen(st.Lhs[0]) == id
}

func valueNames(spec *ast.ValueSpec, id *ast.Ident) bool {
	return slices.Contains(spec.Names, id)
}

func isReadThrough(stack []ast.Node) bool {
	idx := len(stack) - 1
	for idx >= 0 {
		if _, ok := stack[idx].(*ast.ParenExpr); ok {
			idx--
			continue
		}
		break
	}
	if idx < 0 {
		return false
	}
	star, ok := stack[idx].(*ast.StarExpr)
	if !ok {
		return false
	}
	return !starStoredDirectly(stack, star, idx)
}

func starStoredDirectly(stack []ast.Node, star *ast.StarExpr, starIdx int) bool {
	idx := starIdx - 1
	for idx >= 0 {
		if _, ok := stack[idx].(*ast.ParenExpr); ok {
			idx--
			continue
		}
		break
	}
	if idx < 0 {
		return false
	}
	if _, ok := stack[idx].(*ast.IncDecStmt); ok {
		return true
	}
	st, ok := stack[idx].(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, lhs := range st.Lhs {
		if ast.Unparen(lhs) == ast.Node(star) {
			return true
		}
	}
	return false
}
