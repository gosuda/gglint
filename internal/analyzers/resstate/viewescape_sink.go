package resstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func sinkExprs(pass *analysis.Pass, ctx loopCtx) []viewSink {
	var out []viewSink
	ast.Inspect(ctx.body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		sinkStmt(pass, ctx, n, &out)
		return true
	})
	return out
}

func sinkStmt(pass *analysis.Pass, ctx loopCtx, n ast.Node, out *[]viewSink) {
	switch x := n.(type) {
	case *ast.CallExpr:
		sinkAppend(pass, x, out)
	case *ast.SendStmt:
		*out = append(*out, viewSink{expr: x.Value})
	case *ast.AssignStmt:
		for _, e := range appendAssignRHS(pass, ctx, x, nil) {
			*out = append(*out, viewSink{expr: e, assign: x})
		}
	case *ast.ReturnStmt:
		for _, e := range x.Results {
			*out = append(*out, viewSink{expr: e, ret: true})
		}
	}
}

func sinkAppend(pass *analysis.Pass, x *ast.CallExpr, out *[]viewSink) {
	if !isAppendCall(pass, x) {
		return
	}
	for _, e := range appendElemArgs(x, nil) {
		*out = append(*out, viewSink{expr: e})
	}
}

func isAppendCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "append" {
		return false
	}
	obj, ok := pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok && obj.Name() == "append"
}

func appendElemArgs(call *ast.CallExpr, out []ast.Expr) []ast.Expr {
	if len(call.Args) < 2 {
		return out
	}
	if call.Ellipsis.IsValid() {
		return out
	}
	return append(out, call.Args[1:]...)
}

func appendAssignRHS(pass *analysis.Pass, ctx loopCtx, as *ast.AssignStmt, out []ast.Expr) []ast.Expr {
	if len(as.Rhs) == 1 && len(as.Lhs) > 1 {
		if fieldOrMapTarget(pass, ctx, as.Lhs[0]) || outsideVar(pass, ctx.loop, as.Lhs[0]) {
			return append(out, as.Rhs[0])
		}
		return out
	}
	for i, lhs := range as.Lhs {
		if fieldOrMapTarget(pass, ctx, lhs) {
			out = appendRHS(as, i, out)
			continue
		}
		if outsideVar(pass, ctx.loop, lhs) {
			out = appendRHS(as, i, out)
		}
	}
	return out
}

func appendRHS(as *ast.AssignStmt, i int, out []ast.Expr) []ast.Expr {
	if len(as.Rhs) == 1 && len(as.Lhs) > 1 {
		return append(out, as.Rhs[0])
	}
	if len(as.Rhs) == len(as.Lhs) {
		return append(out, as.Rhs[i])
	}
	if len(as.Rhs) == 1 {
		return append(out, as.Rhs[0])
	}
	return out
}

func fieldOrMapTarget(pass *analysis.Pass, ctx loopCtx, lhs ast.Expr) bool {
	switch x := ast.Unparen(lhs).(type) {
	case *ast.SelectorExpr:
		return outerOrEscaped(pass, ctx, x)
	case *ast.IndexExpr:
		if !viewIndexIsMap(pass, x.X) {
			return false
		}
		return outerOrEscaped(pass, ctx, x)
	}
	return false
}

func outerOrEscaped(pass *analysis.Pass, ctx loopCtx, e ast.Expr) bool {
	if !rootInLoop(pass, ctx.loop, e) {
		return true
	}
	v := rootVar(pass, e)
	if v == nil {
		return false
	}
	return loopVarEscapes(pass, ctx, v, e)
}

func rootVar(pass *analysis.Pass, e ast.Expr) *types.Var {
	id := rootIdent(e)
	if id == nil {
		return nil
	}
	return viewVarOf(pass, id)
}

func loopVarEscapes(pass *analysis.Pass, ctx loopCtx, v *types.Var, target ast.Expr) bool {
	if flow.Escapes(pass.TypesInfo, ctx.fn, v) {
		return true
	}
	found := false
	ast.Inspect(ctx.body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		found = escapesAt(pass, ctx, n, target)
		return !found
	})
	return found
}

func escapesAt(pass *analysis.Pass, ctx loopCtx, n ast.Node, target ast.Expr) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		return appendHolds(pass, x, target)
	case *ast.SendStmt:
		return carriesView(pass, x.Value, target)
	case *ast.ReturnStmt:
		return resultsHold(pass, x.Results, target)
	case *ast.AssignStmt:
		return assignHolds(pass, ctx.loop, x, target)
	}
	return false
}

func appendHolds(pass *analysis.Pass, call *ast.CallExpr, target ast.Expr) bool {
	if !isAppendCall(pass, call) {
		return false
	}
	for _, e := range appendElemArgs(call, nil) {
		if carriesView(pass, e, target) {
			return true
		}
	}
	return false
}

func resultsHold(pass *analysis.Pass, results []ast.Expr, target ast.Expr) bool {
	for _, r := range results {
		if carriesView(pass, r, target) {
			return true
		}
	}
	return false
}

func assignHolds(pass *analysis.Pass, loop ast.Node, as *ast.AssignStmt, target ast.Expr) bool {
	if !outerAssign(pass, loop, as.Lhs) {
		return false
	}
	for _, r := range as.Rhs {
		if carriesView(pass, r, target) {
			return true
		}
	}
	return false
}

func outerAssign(pass *analysis.Pass, loop ast.Node, lhs []ast.Expr) bool {
	for _, l := range lhs {
		if outerTarget(pass, loop, l) {
			return true
		}
	}
	return false
}

func outerTarget(pass *analysis.Pass, loop ast.Node, lhs ast.Expr) bool {
	if outsideVar(pass, loop, lhs) {
		return true
	}
	switch x := ast.Unparen(lhs).(type) {
	case *ast.SelectorExpr:
		return !rootInLoop(pass, loop, x)
	case *ast.IndexExpr:
		if !viewIndexIsMap(pass, x.X) {
			return false
		}
		return !rootInLoop(pass, loop, x)
	}
	return false
}

func carriesView(pass *analysis.Pass, escape, target ast.Expr) bool {
	tk, ok := flow.KeyOf(pass.TypesInfo, target)
	if !ok {
		return carriesMapView(pass, escape, target)
	}
	ek, ok := flow.KeyOf(pass.TypesInfo, escape)
	if !ok || ek.Root != tk.Root {
		return false
	}
	return prefixCarry(ek.Path, tk.Path)
}

func prefixCarry(escapePath, targetPath string) bool {
	if escapePath == "" || escapePath == targetPath {
		return true
	}
	if len(targetPath) <= len(escapePath) {
		return false
	}
	return targetPath[:len(escapePath)] == escapePath && targetPath[len(escapePath)] == '.'
}

func carriesMapView(pass *analysis.Pass, escape, target ast.Expr) bool {
	tidx, isIndex := ast.Unparen(target).(*ast.IndexExpr)
	if !isIndex {
		return false
	}
	vroot := rootVar(pass, target)
	if vroot == nil {
		return false
	}
	if ek, isKey := flow.KeyOf(pass.TypesInfo, escape); isKey {
		return ek.Root == vroot && ek.Path == ""
	}
	eidx, isEIndex := ast.Unparen(escape).(*ast.IndexExpr)
	if !isEIndex {
		return false
	}
	if rootVar(pass, escape) != vroot {
		return false
	}
	return types.ExprString(tidx.Index) == types.ExprString(eidx.Index)
}

func rootInLoop(pass *analysis.Pass, loop ast.Node, e ast.Expr) bool {
	id := rootIdent(e)
	if id == nil || id.Name == "_" {
		return false
	}
	v := viewVarOf(pass, id)
	if v == nil {
		return false
	}
	return v.Pos() >= loop.Pos() && v.Pos() <= loop.End()
}

func rootIdent(e ast.Expr) *ast.Ident {
	for {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		default:
			return nil
		}
	}
}

func viewIndexIsMap(pass *analysis.Pass, x ast.Expr) bool {
	t := pass.TypesInfo.Types[ast.Unparen(x)].Type
	if t == nil {
		return false
	}
	if m, ok := types.Unalias(t).Underlying().(*types.Map); ok && m != nil {
		return true
	}
	return false
}

func outsideVar(pass *analysis.Pass, loop ast.Node, lhs ast.Expr) bool {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok || id.Name == "_" {
		return false
	}
	v := viewVarOf(pass, id)
	if v == nil {
		return false
	}
	if v.Pos() >= loop.Pos() && v.Pos() <= loop.End() {
		return false
	}
	return true
}

func viewVarOf(pass *analysis.Pass, id *ast.Ident) *types.Var {
	if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok && v != nil {
		return v
	}
	if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
		return v
	}
	return nil
}

func sinkView(pass *analysis.Pass, s ast.Expr, views map[*types.Var]viewSrc) (view, recv ast.Expr, method string, found bool) {
	inner := ast.Unparen(s)
	if rv, name, ok := viewCallOf(pass, inner); ok {
		return inner, rv, name, true
	}
	id, ok := ast.Unparen(s).(*ast.Ident)
	if !ok {
		return nil, nil, "", false
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return nil, nil, "", false
	}
	src, found := views[v]
	if !found {
		return nil, nil, "", false
	}
	return id, src.recv, src.method, true
}
