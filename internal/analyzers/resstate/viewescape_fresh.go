package resstate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func freshReceiver(pass *analysis.Pass, ctx loopCtx, k flow.Key) bool {
	v, ok := k.Root.(*types.Var)
	if !ok {
		return false
	}
	out := bodyDeclRHS(pass, ctx.body, v)
	if !out.found {
		return false
	}
	if !out.hasValue {
		return true
	}
	return !sameOutsideBuffer(pass, ctx, out.rhs, out.node, k)
}

func sameOutsideBuffer(pass *analysis.Pass, ctx loopCtx, rhs ast.Expr, decl ast.Node, k flow.Key) bool {
	r := stripAddr(rhs)
	if hasCall(r) {
		return false
	}
	root, ok := pathRoot(r)
	if !ok {
		return false
	}
	if !isOuterVar(pass, ctx.loop, ctx.body, root) {
		return false
	}
	if indexMentionsLoopVar(pass, ctx.loop, r) {
		return false
	}
	w, ok := pass.TypesInfo.Uses[root].(*types.Var)
	if !ok {
		return false
	}
	src, ok := pathChain(pass, r, w)
	if !ok {
		return false
	}
	full := slices.Concat(src, keyFieldSteps(k.Path))
	return !assignedEveryPass(pass, ctx, decl, full, w)
}

func keyFieldSteps(path string) []string {
	var out []string
	start := -1
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			if start >= 0 {
				out = append(out, path[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func assignedEveryPass(pass *analysis.Pass, ctx loopCtx, decl ast.Node, full []string, root *types.Var) bool {
	g := viewCachedGraph(pass, ctx)
	if g == nil {
		return true
	}
	avoid := func(n ast.Node) bool {
		return stmtAssignsPath(pass, n, full, root)
	}
	return !g.Reaches(decl, decl, avoid)
}

func stmtAssignsPath(pass *analysis.Pass, n ast.Node, full []string, root *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return lhsHitsPath(pass, x.Lhs, full, root)
	case *ast.IncDecStmt:
		return pathAffectsTarget(pass, x.X, full, root)
	case *ast.RangeStmt:
		return pathAffectsTarget(pass, x.Key, full, root) || pathAffectsTarget(pass, x.Value, full, root)
	}
	return false
}

func lhsHitsPath(pass *analysis.Pass, lhs []ast.Expr, full []string, root *types.Var) bool {
	for _, l := range lhs {
		if pathAffectsTarget(pass, l, full, root) {
			return true
		}
	}
	return false
}

func pathAffectsTarget(pass *analysis.Pass, e ast.Expr, full []string, root *types.Var) bool {
	if e == nil {
		return false
	}
	lc, ok := pathChain(pass, e, root)
	if !ok {
		return unspellableRebinds(pass, e, root)
	}
	for i, s := range lc {
		if i >= len(full) {
			return true
		}
		if stepsMatch(s, full[i]) {
			continue
		}
		return false
	}
	return true
}

func isIndexStep(s string) bool {
	return s != "" && s[0] == '['
}

func stepsMatch(a, b string) bool {
	if a == b {
		return true
	}
	return (a == "[]" || b == "[]") && isIndexStep(a) && isIndexStep(b)
}

func constIndexStep(pass *analysis.Pass, e ast.Expr) string {
	if e == nil {
		return "[]"
	}
	tv, ok := pass.TypesInfo.Types[ast.Unparen(e)]
	if !ok || tv.Value == nil {
		return "[]"
	}
	return "[" + tv.Value.ExactString() + "]"
}

func pathChain(pass *analysis.Pass, e ast.Expr, root *types.Var) ([]string, bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if pass.TypesInfo.Uses[x] == root {
			return []string{}, true
		}
		return nil, false
	case *ast.SelectorExpr:
		steps, ok := pathChain(pass, x.X, root)
		if !ok {
			return nil, false
		}
		return append(steps, selectorSteps(pass, x)...), true
	case *ast.IndexExpr:
		steps, ok := pathChain(pass, x.X, root)
		if !ok {
			return nil, false
		}
		return append(steps, constIndexStep(pass, x.Index)), true
	case *ast.SliceExpr:
		steps, ok := pathChain(pass, x.X, root)
		if !ok {
			return nil, false
		}
		return append(steps, constIndexStep(pass, x.Low)), true
	}
	return nil, false
}

func selectorSteps(pass *analysis.Pass, x *ast.SelectorExpr) []string {
	sel := pass.TypesInfo.Selections[x]
	if sel == nil {
		return []string{x.Sel.Name}
	}
	idx := sel.Index()
	if len(idx) <= 1 {
		return []string{x.Sel.Name}
	}
	t := pass.TypesInfo.TypeOf(x.X)
	var out []string
	for _, i := range idx {
		st, ok := structOf(t)
		if !ok || i < 0 || i >= st.NumFields() {
			return []string{x.Sel.Name}
		}
		out = append(out, st.Field(i).Name())
		t = st.Field(i).Type()
	}
	return out
}

func structOf(t types.Type) (*types.Struct, bool) {
	if t == nil {
		return nil, false
	}
	u := types.Unalias(t)
	if p, ok := u.(*types.Pointer); ok {
		u = types.Unalias(p.Elem())
	}
	st, ok := u.Underlying().(*types.Struct)
	return st, ok
}

func unspellableRebinds(pass *analysis.Pass, e ast.Expr, root *types.Var) bool {
	v, ok := targetBaseVar(pass, e)
	if ok {
		return v == root
	}
	return mentionsRootVar(pass, e, root)
}

func mentionsRootVar(pass *analysis.Pass, e ast.Expr, root *types.Var) bool {
	if e == nil {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && v == root {
			found = true
			return false
		}
		return true
	})
	return found
}

func targetBaseVar(pass *analysis.Pass, e ast.Expr) (*types.Var, bool) {
	for {
		if id, ok := ast.Unparen(e).(*ast.Ident); ok {
			v, ok := pass.TypesInfo.Uses[id].(*types.Var)
			return v, ok
		}
		next, ok := targetBaseStep(e)
		if !ok {
			return nil, false
		}
		e = next
	}
}

func targetBaseStep(e ast.Expr) (ast.Expr, bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		return x.X, true
	case *ast.IndexExpr:
		return x.X, true
	case *ast.SliceExpr:
		return x.X, true
	case *ast.StarExpr:
		return x.X, true
	case *ast.UnaryExpr:
		if x.Op != token.AND {
			return nil, false
		}
		return x.X, true
	}
	return nil, false
}

func stripAddr(e ast.Expr) ast.Expr {
	r := ast.Unparen(e)
	for {
		u, ok := r.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return r
		}
		r = ast.Unparen(u.X)
	}
}

func hasCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

func pathRoot(e ast.Expr) (*ast.Ident, bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x, true
	case *ast.SelectorExpr:
		return pathRoot(x.X)
	case *ast.IndexExpr:
		return pathRoot(x.X)
	case *ast.SliceExpr:
		return pathRoot(x.X)
	}
	return nil, false
}

func isOuterVar(pass *analysis.Pass, loop, body ast.Node, id *ast.Ident) bool {
	w, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return false
	}
	if loopVarSet(pass, loop)[w] {
		return false
	}
	return w.Pos() < body.Pos() || w.Pos() > body.End()
}

func indexMentionsLoopVar(pass *analysis.Pass, loop ast.Node, e ast.Expr) bool {
	vars := loopVarSet(pass, loop)
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.IndexExpr:
			if exprUsesLoopVar(pass, x.Index, vars) {
				found = true
				return false
			}
		case *ast.SliceExpr:
			if exprUsesLoopVar(pass, x.Low, vars) || exprUsesLoopVar(pass, x.High, vars) || exprUsesLoopVar(pass, x.Max, vars) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func loopVarSet(pass *analysis.Pass, loop ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	var init ast.Node
	if fs, ok := loop.(*ast.ForStmt); ok {
		init = fs.Init
	}
	ast.Inspect(loop, func(n ast.Node) bool {
		if n == init {
			return false
		}
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				addDefIdents(pass, x.Lhs, out)
			}
		case *ast.ValueSpec:
			addDefNames(pass, x, out)
		case *ast.RangeStmt:
			addRangeDefs(pass, x, out)
		}
		return true
	})
	addReassignedInitVars(pass, loop, out)
	return out
}

func addReassignedInitVars(pass *analysis.Pass, loop ast.Node, out map[*types.Var]bool) {
	fs, ok := loop.(*ast.ForStmt)
	if !ok {
		return
	}
	as, ok := fs.Init.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return
	}
	for _, l := range as.Lhs {
		addReassignedInitVar(pass, fs, l, out)
	}
}

func addReassignedInitVar(pass *analysis.Pass, fs *ast.ForStmt, l ast.Expr, out map[*types.Var]bool) {
	id, ok := ast.Unparen(l).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.Defs[id].(*types.Var)
	if !ok {
		return
	}
	if assignedIn(pass, fs.Post, v) || assignedIn(pass, fs.Body, v) {
		out[v] = true
	}
}

func assignedIn(pass *analysis.Pass, root ast.Node, v *types.Var) bool {
	if root == nil {
		return false
	}
	assigned := false
	ast.Inspect(root, func(n ast.Node) bool {
		if assigned {
			return false
		}
		if assignsRoot(pass, n, v) {
			assigned = true
			return false
		}
		return true
	})
	return assigned
}

func addDefIdents(pass *analysis.Pass, lhs []ast.Expr, out map[*types.Var]bool) {
	for _, l := range lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		addDefIdent(pass, id, out)
	}
}

func addDefNames(pass *analysis.Pass, x *ast.ValueSpec, out map[*types.Var]bool) {
	for _, id := range x.Names {
		addDefIdent(pass, id, out)
	}
}

func addRangeDefs(pass *analysis.Pass, x *ast.RangeStmt, out map[*types.Var]bool) {
	for _, e := range []ast.Expr{x.Key, x.Value} {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			continue
		}
		addDefIdent(pass, id, out)
	}
}

func addDefIdent(pass *analysis.Pass, id *ast.Ident, out map[*types.Var]bool) {
	if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok {
		out[v] = true
	}
}

func exprUsesLoopVar(pass *analysis.Pass, e ast.Expr, vars map[*types.Var]bool) bool {
	if e == nil {
		return false
	}
	used := false
	ast.Inspect(e, func(n ast.Node) bool {
		if used {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && vars[v] {
			used = true
			return false
		}
		return true
	})
	return used
}

func bodyDeclRHS(pass *analysis.Pass, body ast.Node, v *types.Var) declOutcome {
	found := declOutcome{}
	ast.Inspect(body, func(n ast.Node) bool {
		if found.found {
			return false
		}
		if out := declRHS(pass, n, v); out.found {
			found = out
			return false
		}
		return true
	})
	return found
}

type declOutcome struct {
	rhs      ast.Expr
	hasValue bool
	found    bool
	pos      token.Pos
	node     ast.Node
}

func declRHS(pass *analysis.Pass, n ast.Node, v *types.Var) declOutcome {
	switch x := n.(type) {
	case *ast.ValueSpec:
		return specOutcome(pass, x, v)
	case *ast.AssignStmt:
		return defineOutcome(pass, x, v)
	}
	return declOutcome{}
}

func specOutcome(pass *analysis.Pass, x *ast.ValueSpec, v *types.Var) declOutcome {
	for i, id := range x.Names {
		if pass.TypesInfo.Defs[id] == v {
			if len(x.Values) == 0 {
				return declOutcome{found: true, pos: x.Pos(), node: x}
			}
			if len(x.Names) == len(x.Values) {
				return declOutcome{rhs: x.Values[i], hasValue: true, found: true, pos: x.Pos(), node: x}
			}
			return declOutcome{rhs: x.Values[0], hasValue: true, found: true, pos: x.Pos(), node: x}
		}
	}
	return declOutcome{}
}

func defineOutcome(pass *analysis.Pass, x *ast.AssignStmt, v *types.Var) declOutcome {
	if x.Tok != token.DEFINE {
		return declOutcome{}
	}
	for i, l := range x.Lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.Defs[id] == v {
			if len(x.Rhs) == 0 {
				return declOutcome{found: true, pos: x.Pos(), node: x}
			}
			if len(x.Lhs) == len(x.Rhs) {
				return declOutcome{rhs: x.Rhs[i], hasValue: true, found: true, pos: x.Pos(), node: x}
			}
			return declOutcome{rhs: x.Rhs[0], hasValue: true, found: true, pos: x.Pos(), node: x}
		}
	}
	return declOutcome{}
}

func freshInvalidAfter(pass *analysis.Pass, ctx loopCtx, view ast.Expr, recvKey flow.Key, viewMethod string) bool {
	g := viewCachedGraph(pass, ctx)
	if g == nil {
		return false
	}
	avoid := reassignAvoid(pass, recvKey)
	for _, k := range nearestCalls(viewCachedInvalid(pass, ctx, recvKey, viewMethod), view.Pos()) {
		if g.Reaches(view, k, avoid) {
			return true
		}
	}
	return false
}

func declaresVar(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return definesLHS(pass, x, v)
	case *ast.ValueSpec:
		return definesSpec(pass, x, v)
	case *ast.DeclStmt:
		return definesGenDecl(pass, x.Decl, v)
	}
	return false
}

func definesLHS(pass *analysis.Pass, x *ast.AssignStmt, v *types.Var) bool {
	if x.Tok != token.DEFINE {
		return false
	}
	for _, l := range x.Lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.Defs[id] == v {
			return true
		}
	}
	return false
}

func definesSpec(pass *analysis.Pass, x *ast.ValueSpec, v *types.Var) bool {
	for _, id := range x.Names {
		if pass.TypesInfo.Defs[id] == v {
			return true
		}
	}
	return false
}

func definesGenDecl(pass *analysis.Pass, d ast.Decl, v *types.Var) bool {
	g, ok := d.(*ast.GenDecl)
	if !ok {
		return false
	}
	for _, s := range g.Specs {
		if vs, ok := s.(*ast.ValueSpec); ok && definesSpec(pass, vs, v) {
			return true
		}
	}
	return false
}
