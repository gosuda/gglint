package resstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func outerViewVar(pass *analysis.Pass, loop ast.Node, as *ast.AssignStmt, expr ast.Expr) (*types.Var, bool) {
	if len(as.Rhs) == 1 {
		if as.Rhs[0] != expr {
			return nil, false
		}
		return outerIdent(pass, loop, as.Lhs[0])
	}
	if len(as.Rhs) != len(as.Lhs) {
		return nil, false
	}
	for i, r := range as.Rhs {
		if r != expr {
			continue
		}
		return outerIdent(pass, loop, as.Lhs[i])
	}
	return nil, false
}

func outerIdent(pass *analysis.Pass, loop ast.Node, lhs ast.Expr) (*types.Var, bool) {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, false
	}
	if !outsideVar(pass, loop, lhs) {
		return nil, false
	}
	v := viewVarOf(pass, id)
	if v == nil {
		return nil, false
	}
	return v, true
}

func outerAssignLive(pass *analysis.Pass, ctx loopCtx, sink *ast.AssignStmt, v *types.Var, recvKey flow.Key, viewMethod string) bool {
	g := viewCachedGraph(pass, ctx)
	if g == nil {
		return false
	}
	calls := viewCachedInvalid(pass, ctx, recvKey, viewMethod)
	if len(calls) == 0 {
		return false
	}
	avoid := func(n ast.Node) bool {
		return viewAssignsFast(pass, ctx, n, v)
	}
	for _, k := range calls {
		if viewCallInAssign(pass, ctx, k, v) {
			continue
		}
		if !g.Reaches(sink, k, avoid) {
			continue
		}
		if hasLiveRead(pass, ctx, g, k, v, avoid) {
			return true
		}
	}
	return false
}

func invalidCalls(pass *analysis.Pass, fn ast.Node, recvKey flow.Key, viewMethod string) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != fn {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if invalidCallMatch(pass, call, recvKey, viewMethod) {
			out = append(out, call)
		}
		return true
	})
	return out
}

func invalidCallMatch(pass *analysis.Pass, call *ast.CallExpr, recvKey flow.Key, viewMethod string) bool {
	name := viewInvalidName(pass, call)
	if name == "" || name == viewMethod {
		return false
	}
	k, ok := flow.MethodKey(pass.TypesInfo, call)
	if !ok || k != recvKey {
		return false
	}
	return true
}

func hasLiveRead(pass *analysis.Pass, ctx loopCtx, g *flow.Graph, k ast.Node, v *types.Var, avoid func(ast.Node) bool) bool {
	if liveReturn(pass, ctx, g, k, v, avoid) {
		return true
	}
	return liveIdent(pass, ctx, g, k, v, avoid)
}

func liveReturn(pass *analysis.Pass, ctx loopCtx, g *flow.Graph, k ast.Node, v *types.Var, avoid func(ast.Node) bool) bool {
	found := false
	ast.Inspect(ctx.fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != ctx.fn {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if returnReads(pass, ret, v, ctx.fn) && g.Reaches(k, ret, avoid) {
			found = true
		}
		return !found
	})
	return found
}

func returnReads(pass *analysis.Pass, ret *ast.ReturnStmt, v *types.Var, fn ast.Node) bool {
	if len(ret.Results) == 0 {
		return isNamedResult(pass, fn, v)
	}
	for _, r := range ret.Results {
		if mentionsVar(pass, r, v) {
			return true
		}
	}
	return false
}

func liveIdent(pass *analysis.Pass, ctx loopCtx, g *flow.Graph, k ast.Node, v *types.Var, avoid func(ast.Node) bool) bool {
	skip := viewCachedLHS(pass, ctx, v)
	discard := viewCachedDiscard(pass, ctx, v)
	found := false
	ast.Inspect(ctx.fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != ctx.fn {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] != v {
			return true
		}
		if skip[id] || discard[id] {
			return true
		}
		if g.Reaches(k, id, avoid) {
			found = true
		}
		return !found
	})
	return found
}

func discardRHSIdents(pass *analysis.Pass, fn ast.Node, v *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, isLit := n.(*ast.FuncLit); isLit && lit != fn {
			return false
		}
		markDiscardRHS(pass, n, v, out)
		return true
	})
	return out
}

func markDiscardRHS(pass *analysis.Pass, n ast.Node, v *types.Var, out map[*ast.Ident]bool) {
	as, ok := n.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return
	}
	if !isBlankIdent(as.Lhs[0]) {
		return
	}
	rid, ok := ast.Unparen(as.Rhs[0]).(*ast.Ident)
	if ok && pass.TypesInfo.Uses[rid] == v {
		out[rid] = true
	}
}

func assignLHSIdents(pass *analysis.Pass, fn ast.Node, v *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, isLit := n.(*ast.FuncLit); isLit && lit != fn {
			return false
		}
		as, isAssign := n.(*ast.AssignStmt)
		if !isAssign {
			return true
		}
		markLHSIds(pass, fn, as, v, out)
		return true
	})
	return out
}

func markLHSIds(pass *analysis.Pass, fn ast.Node, as *ast.AssignStmt, v *types.Var, out map[*ast.Ident]bool) {
	if !assignsVar(pass, fn, as, v) {
		return
	}
	for _, lhs := range as.Lhs {
		id, isIdent := ast.Unparen(lhs).(*ast.Ident)
		if !isIdent {
			continue
		}
		if pass.TypesInfo.Uses[id] == v {
			out[id] = true
		}
	}
}

func isNamedResult(pass *analysis.Pass, fn ast.Node, v *types.Var) bool {
	res := funcResults(fn)
	if res == nil {
		return false
	}
	for _, f := range res.List {
		if resultNamesHold(pass, f, v) {
			return true
		}
	}
	return false
}

func funcResults(fn ast.Node) *ast.FieldList {
	switch x := fn.(type) {
	case *ast.FuncDecl:
		if x.Type == nil {
			return nil
		}
		return x.Type.Results
	case *ast.FuncLit:
		if x.Type == nil {
			return nil
		}
		return x.Type.Results
	default:
		return nil
	}
}

func resultNamesHold(pass *analysis.Pass, f *ast.Field, v *types.Var) bool {
	for _, n := range f.Names {
		if pass.TypesInfo.Defs[n] == v {
			return true
		}
	}
	return false
}
