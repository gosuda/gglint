package alias

import (
	"go/ast"
	"go/token"
	"go/types"

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
