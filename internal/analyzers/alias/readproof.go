package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type clobberQuery struct {
	graph  *flow.Graph
	info   *types.Info
	target *types.Var
	from   ast.Node
	after  token.Pos
	bound  int
	end    int
	skip   map[*ast.Ident]bool
	stores map[*ast.Ident]bool
	clears map[*ast.Ident]bool
	bases  map[*ast.Ident]bool
	writes map[*ast.Ident]bool
	avoid  func(ast.Node) bool
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
		graph:  g,
		info:   info,
		target: a,
		from:   site.from,
		after:  site.after,
		bound:  site.bound,
		end:    site.end,
		skip:   lenCapIdents(info, fn, a),
		stores: plainStoreBases(info, fn, a),
		clears: clearArgIdents(info, fn, a),
		bases:  indexSliceBases(info, fn, a),
		writes: assignTargetIdents(info, fn, a),
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
		switch t := n.(type) {
		case *ast.IndexExpr:
			hit = q.indexHit(t)
		case *ast.SliceExpr:
			hit = q.sliceHit(t)
		case *ast.Ident:
			hit = q.identHit(t)
		}
		return !hit
	})
	return hit
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
	if t.Pos() <= q.after {
		return false
	}
	if q.sliceOutside(t) {
		return false
	}
	return q.graph.Reaches(q.from, t, q.avoid)
}

func (q *clobberQuery) identHit(id *ast.Ident) bool {
	if q.info.Uses[id] != q.target {
		return false
	}
	if id.Pos() <= q.after {
		return false
	}
	if q.skip[id] || q.stores[id] || q.clears[id] || q.bases[id] || q.writes[id] {
		return false
	}
	return q.graph.Reaches(q.from, id, q.avoid)
}

func (q *clobberQuery) indexOutside(idx *ast.IndexExpr) bool {
	k, ok := constInt(q.info, idx.Index)
	if !ok {
		return false
	}
	return k < q.bound || k >= q.end
}

func (q *clobberQuery) sliceOutside(s *ast.SliceExpr) bool {
	low := 0
	if s.Low != nil {
		v, ok := constInt(q.info, s.Low)
		if !ok {
			return false
		}
		low = v
	}
	if s.High != nil {
		h, ok := constInt(q.info, s.High)
		if !ok {
			return low >= q.end
		}
		if h <= q.bound {
			return true
		}
		return low >= q.end
	}
	return low >= q.end
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

func indexSliceBases(info *types.Info, fn ast.Node, a *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch t := n.(type) {
		case *ast.IndexExpr:
			markBase(info, t.X, a, out)
		case *ast.SliceExpr:
			markBase(info, t.X, a, out)
		}
		return true
	})
	return out
}

func markBase(info *types.Info, x ast.Expr, a *types.Var, out map[*ast.Ident]bool) {
	v, ok := identVar(info, x)
	if !ok || v != a {
		return
	}
	if id, ok := ast.Unparen(x).(*ast.Ident); ok {
		out[id] = true
	}
}

func assignTargetIdents(info *types.Info, fn ast.Node, a *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range st.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
				if v, ok := varOf(info, id); ok && v == a {
					out[id] = true
				}
			}
		}
		return true
	})
	return out
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
