package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type readSite struct {
	from  ast.Node
	after token.Pos
	bound int
	end   int
}

func overwrittenReadAfter(scope *appendScope, a *types.Var, site readSite) bool {
	if scope.graph == nil {
		return false
	}
	kills := scope.assigns[a]
	avoid := func(n ast.Node) bool {
		for _, k := range kills {
			if n == k {
				return true
			}
		}
		return false
	}
	if scopeIndexHit(scope, a, site, avoid) {
		return true
	}
	if scopeSliceHit(scope, a, site, avoid) {
		return true
	}
	return scopeWholeHit(scope, a, site, avoid)
}

func scopeIndexHit(scope *appendScope, a *types.Var, site readSite, avoid func(ast.Node) bool) bool {
	for _, t := range scope.indexReads[a] {
		if t.Pos() <= site.after {
			continue
		}
		if scopeIndexOutside(scope.info, t, site.bound, site.end) {
			continue
		}
		if scope.graph.Reaches(site.from, t, avoid) {
			return true
		}
	}
	return false
}

func scopeSliceHit(scope *appendScope, a *types.Var, site readSite, avoid func(ast.Node) bool) bool {
	for _, t := range scope.sliceReads[a] {
		if t.Pos() <= site.after {
			continue
		}
		if scopeSliceOutside(scope.info, t, site.bound, site.end) {
			continue
		}
		if scope.graph.Reaches(site.from, t, avoid) {
			return true
		}
	}
	return false
}

func scopeWholeHit(scope *appendScope, a *types.Var, site readSite, avoid func(ast.Node) bool) bool {
	for _, id := range scope.wholeReads[a] {
		if id.Pos() <= site.after {
			continue
		}
		if scope.graph.Reaches(site.from, id, avoid) {
			return true
		}
	}
	return false
}

func scopeIndexOutside(info *types.Info, idx *ast.IndexExpr, bound, end int) bool {
	k, ok := constInt(info, idx.Index)
	if !ok {
		return true
	}
	return k < bound || k >= end
}

func scopeSliceOutside(info *types.Info, s *ast.SliceExpr, bound, end int) bool {
	low := 0
	if s.Low != nil {
		v, ok := constInt(info, s.Low)
		if !ok {
			return true
		}
		low = v
	}
	if s.High != nil {
		h, ok := constInt(info, s.High)
		if !ok {
			return true
		}
		if h <= bound {
			return true
		}
		return low >= end
	}
	if s.Max != nil {
		if _, ok := constInt(info, s.Max); !ok {
			return true
		}
	}
	return low >= end
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
