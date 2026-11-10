package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type clobberQuery struct {
	graph       *flow.Graph
	info        *types.Info
	target      *types.Var
	from        ast.Node
	after       token.Pos
	bound       int
	end         int
	stores      map[*ast.Ident]bool
	clears      map[*ast.Ident]bool
	copyDests   map[ast.Node]bool
	clearSlices map[ast.Node]bool
	avoid       func(ast.Node) bool
}

type readSite struct {
	from  ast.Node
	after token.Pos
	bound int
	end   int
}

func overwrittenReadAfter(pass *analysis.Pass, fn ast.Node, a *types.Var, site readSite) bool {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return false
	}
	info := pass.TypesInfo
	q := &clobberQuery{
		graph:       g,
		info:        info,
		target:      a,
		from:        site.from,
		after:       site.after,
		bound:       site.bound,
		end:         site.end,
		stores:      plainStoreBases(info, fn, a),
		clears:      clearArgIdents(info, fn, a),
		copyDests:   copyDestNodes(info, fn),
		clearSlices: clearSliceNodes(info, fn),
	}
	q.avoid = func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		return ok && assignsVar(info, st, a)
	}
	hit := false
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		hit = q.checkNode(n)
		return !hit
	})
	return hit
}

func (q *clobberQuery) checkNode(n ast.Node) bool {
	switch t := n.(type) {
	case *ast.IndexExpr:
		return q.indexHit(t)
	case *ast.SliceExpr:
		return q.sliceHit(t)
	default:
		return q.wholeNode(n)
	}
}

func (q *clobberQuery) wholeNode(n ast.Node) bool {
	switch t := n.(type) {
	case *ast.CallExpr:
		return q.callWholeRead(t)
	case *ast.ReturnStmt:
		return q.returnWholeRead(t)
	case *ast.RangeStmt:
		return q.rangeWholeRead(t)
	case *ast.AssignStmt:
		return q.assignWholeRead(t)
	case *ast.SendStmt:
		return q.sendWholeRead(t)
	case *ast.CompositeLit:
		return q.literalWholeRead(t)
	case *ast.ValueSpec:
		return q.valueWholeRead(t)
	}
	return false
}

func (q *clobberQuery) indexHit(t *ast.IndexExpr) bool {
	v, ok := identVar(q.info, t.X)
	if !ok || v != q.target {
		return false
	}
	id, ok := ast.Unparen(t.X).(*ast.Ident)
	if !ok || q.stores[id] {
		return false
	}
	if t.Pos() <= q.after {
		return false
	}
	if q.indexOutside(t) {
		return false
	}
	return q.graph.Reaches(q.from, t, q.avoid)
}

func (q *clobberQuery) sliceHit(t *ast.SliceExpr) bool {
	v, ok := identVar(q.info, t.X)
	if !ok || v != q.target {
		return false
	}
	id, ok := ast.Unparen(t.X).(*ast.Ident)
	if !ok || q.clears[id] {
		return false
	}
	if q.copyDests[t] || q.clearSlices[t] {
		return false
	}
	if t.Pos() <= q.after {
		return false
	}
	if q.sliceOutside(t) {
		return false
	}
	return q.graph.Reaches(q.from, t, q.avoid)
}

func (q *clobberQuery) wholeIdent(e ast.Expr) (*ast.Ident, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil, false
	}
	if v, ok := varOf(q.info, id); !ok || v != q.target {
		return nil, false
	}
	return id, true
}

func (q *clobberQuery) reachesIdent(id *ast.Ident) bool {
	if id.Pos() <= q.after {
		return false
	}
	return q.graph.Reaches(q.from, id, q.avoid)
}

func (q *clobberQuery) callWholeRead(call *ast.CallExpr) bool {
	if isLenCapCall(q.info, call) || isClearCall(q.info, call) {
		return false
	}
	isCopy := isCopyCall(q.info, call)
	for i, arg := range call.Args {
		if isCopy && i == 0 {
			continue
		}
		id, ok := q.wholeIdent(arg)
		if !ok {
			continue
		}
		if q.reachesIdent(id) {
			return true
		}
	}
	return false
}

func (q *clobberQuery) returnWholeRead(st *ast.ReturnStmt) bool {
	for _, res := range st.Results {
		id, ok := q.wholeIdent(res)
		if !ok {
			continue
		}
		if q.reachesIdent(id) {
			return true
		}
	}
	return false
}

func (q *clobberQuery) rangeWholeRead(st *ast.RangeStmt) bool {
	if st.Value == nil {
		return false
	}
	if id, ok := ast.Unparen(st.Value).(*ast.Ident); ok && id.Name == "_" {
		return false
	}
	id, ok := q.wholeIdent(st.X)
	if !ok {
		return false
	}
	return q.reachesIdent(id)
}

func (q *clobberQuery) assignWholeRead(st *ast.AssignStmt) bool {
	for _, rhs := range st.Rhs {
		id, ok := q.wholeIdent(rhs)
		if !ok {
			continue
		}
		if q.reachesIdent(id) {
			return true
		}
	}
	return false
}

func (q *clobberQuery) sendWholeRead(st *ast.SendStmt) bool {
	id, ok := q.wholeIdent(st.Value)
	if !ok {
		return false
	}
	return q.reachesIdent(id)
}

func (q *clobberQuery) literalWholeRead(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if id, ok := q.wholeIdent(elt); ok && q.reachesIdent(id) {
			return true
		}
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if id, ok := q.wholeIdent(kv.Value); ok && q.reachesIdent(id) {
				return true
			}
		}
	}
	return false
}

func (q *clobberQuery) valueWholeRead(spec *ast.ValueSpec) bool {
	for _, val := range spec.Values {
		id, ok := q.wholeIdent(val)
		if !ok {
			continue
		}
		if q.reachesIdent(id) {
			return true
		}
	}
	return false
}

func (q *clobberQuery) indexOutside(idx *ast.IndexExpr) bool {
	k, ok := constInt(q.info, idx.Index)
	if !ok {
		return true
	}
	return k < q.bound || k >= q.end
}

func (q *clobberQuery) sliceOutside(s *ast.SliceExpr) bool {
	low := 0
	if s.Low != nil {
		v, ok := constInt(q.info, s.Low)
		if !ok {
			return true
		}
		low = v
	}
	if s.High != nil {
		h, ok := constInt(q.info, s.High)
		if !ok {
			return true
		}
		if h <= q.bound {
			return true
		}
		return low >= q.end
	}
	if s.Max != nil {
		if _, ok := constInt(q.info, s.Max); !ok {
			return true
		}
	}
	return low >= q.end
}

func isCopyCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "copy"
}

func copyDestNodes(info *types.Info, fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isCopyCall(info, call) || len(call.Args) == 0 {
			return true
		}
		out[ast.Unparen(call.Args[0])] = true
		return true
	})
	return out
}

func clearSliceNodes(info *types.Info, fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isClearCall(info, call) {
			return true
		}
		for _, arg := range call.Args {
			out[ast.Unparen(arg)] = true
		}
		return true
	})
	return out
}

func plainStoreBases(info *types.Info, fn ast.Node, a *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok || st.Tok != token.ASSIGN {
			return true
		}
		markPlainStore(info, st, a, out)
		return true
	})
	return out
}

func markPlainStore(info *types.Info, st *ast.AssignStmt, a *types.Var, out map[*ast.Ident]bool) {
	for _, lhs := range st.Lhs {
		markPlainLHS(info, lhs, a, out)
	}
}

func markPlainLHS(info *types.Info, lhs ast.Expr, a *types.Var, out map[*ast.Ident]bool) {
	idx, ok := ast.Unparen(lhs).(*ast.IndexExpr)
	if !ok {
		return
	}
	v, ok := identVar(info, idx.X)
	if !ok || v != a {
		return
	}
	if id, ok := ast.Unparen(idx.X).(*ast.Ident); ok {
		out[id] = true
	}
}

func clearArgIdents(info *types.Info, fn ast.Node, a *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isClearCall(info, call) {
			return true
		}
		markClearArgs(info, call, a, out)
		return true
	})
	return out
}

func markClearArgs(info *types.Info, call *ast.CallExpr, a *types.Var, out map[*ast.Ident]bool) {
	for _, arg := range call.Args {
		ast.Inspect(arg, func(x ast.Node) bool {
			markClearIdent(info, x, a, out)
			return true
		})
	}
}

func markClearIdent(info *types.Info, x ast.Node, a *types.Var, out map[*ast.Ident]bool) {
	id, ok := x.(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := varOf(info, id); ok && v == a {
		out[id] = true
	}
}

func isClearCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "clear"
}

func hasCoveringIndex(pass *analysis.Pass, fn ast.Node, a *types.Var, from ast.Node, threshold int) bool {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return false
	}
	avoid := func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		return ok && assignsVar(pass.TypesInfo, st, a)
	}
	found := false
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		idx, ok := n.(*ast.IndexExpr)
		if !ok || !indexOf(pass.TypesInfo, idx, a, threshold) {
			return true
		}
		if idx.Pos() <= from.End() {
			return true
		}
		if g.Reaches(from, idx, avoid) {
			found = true
		}
		return !found
	})
	return found
}
