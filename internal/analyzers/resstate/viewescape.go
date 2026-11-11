package resstate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

const (
	viewBufioPath       = "bufio"
	viewBytesPath       = "bytes"
	viewScannerName     = "Scanner"
	viewReaderName      = "Reader"
	viewBufferName      = "Buffer"
	viewBytesName       = "Bytes"
	viewPeekName        = "Peek"
	viewReadSliceName   = "ReadSlice"
	viewScanName        = "Scan"
	viewResetName       = "Reset"
	viewWriteStringName = "WriteString"
	viewDiscardName     = "Discard"
)

func runViewescape(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		viewFunc(pass, cur.Node())
	}
	return nil, nil
}

func viewFunc(pass *analysis.Pass, fn ast.Node) {
	seen := map[token.Pos]bool{}
	var views map[*types.Var]viewSrc
	ready := false
	cache := newViewCache()
	ast.Inspect(fn, func(n ast.Node) bool {
		if n != fn {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
		}
		loop, ok := loopBody(n)
		if !ok {
			return true
		}
		if !ready {
			views = viewDefs(pass, fn)
			ready = true
		}
		viewLoop(pass, loopCtx{fn: fn, loop: n, body: loop, cache: cache}, seen, views)
		return true
	})
}

func loopBody(n ast.Node) (ast.Node, bool) {
	switch x := n.(type) {
	case *ast.ForStmt:
		return x.Body, true
	case *ast.RangeStmt:
		return x.Body, true
	}
	return nil, false
}

type viewSink struct {
	expr   ast.Expr
	ret    bool
	assign *ast.AssignStmt
}

func viewLoop(pass *analysis.Pass, ctx loopCtx, seen map[token.Pos]bool, views map[*types.Var]viewSrc) {
	invalid := invalidReceivers(pass, ctx.loop)
	if len(invalid) == 0 {
		return
	}
	for _, s := range sinkExprs(pass, ctx) {
		v, recvExpr, recvKey, viewMethod, ready := sinkReady(pass, ctx, views, s)
		if !ready {
			continue
		}
		if tryPoolReport(pass, ctx, poolNeed{s: s, view: v, recv: recvExpr, key: recvKey}, seen) {
			continue
		}
		method, found := reachMethodName(pass, ctx, s.expr, recvKey, viewMethod)
		if !found || seen[s.expr.Pos()] {
			continue
		}
		seen[s.expr.Pos()] = true
		pass.Reportf(s.expr.Pos(), "%s points into the buffer of %s, and a later %s call may overwrite it: store a copy made with bytes.Clone", types.ExprString(v), types.ExprString(recvExpr), method)
	}
}

func reachMethodName(pass *analysis.Pass, ctx loopCtx, view ast.Expr, recvKey flow.Key, viewMethod string) (string, bool) {
	g := viewCachedGraph(pass, ctx)
	if g == nil {
		return "", false
	}
	calls := viewCachedInvalid(pass, ctx, recvKey, viewMethod)
	ordered := append([]*ast.CallExpr(nil), calls...)
	slices.SortFunc(ordered, func(a, b *ast.CallExpr) int {
		return int(a.Pos() - b.Pos())
	})
	avoid := assignAvoid(pass, recvKey)
	if name, ok := laterReachableName(pass, g, ordered, view, avoid, viewMethod); ok {
		return name, true
	}
	return firstReachableName(pass, g, ordered, view, avoid, viewMethod)
}

func laterReachableName(pass *analysis.Pass, g *flow.Graph, calls []*ast.CallExpr, view ast.Expr, avoid func(ast.Node) bool, viewMethod string) (string, bool) {
	for _, k := range calls {
		if k.Pos() <= view.Pos() {
			continue
		}
		if g.Reaches(view, k, avoid) {
			if name, ok := invalidName(pass, k, viewMethod); ok {
				return name, true
			}
		}
	}
	return "", false
}

func firstReachableName(pass *analysis.Pass, g *flow.Graph, calls []*ast.CallExpr, view ast.Expr, avoid func(ast.Node) bool, viewMethod string) (string, bool) {
	for _, k := range calls {
		if g.Reaches(view, k, avoid) {
			if name, ok := invalidName(pass, k, viewMethod); ok {
				return name, true
			}
		}
	}
	return "", false
}

func invalidName(pass *analysis.Pass, call *ast.CallExpr, viewMethod string) (string, bool) {
	name := viewInvalidName(pass, call)
	if name == "" || name == viewMethod {
		return "", false
	}
	return name, true
}

func sinkReady(pass *analysis.Pass, ctx loopCtx, views map[*types.Var]viewSrc, s viewSink) (view, recvExpr ast.Expr, recvKey flow.Key, viewMethod string, ready bool) {
	v, recv, method, ok := sinkView(pass, s.expr, views)
	if !ok {
		return nil, nil, flow.Key{}, "", false
	}
	if shadowedByRange(pass, ctx.fn, s.expr, v, views) {
		return nil, nil, flow.Key{}, "", false
	}
	k, ok := flow.KeyOf(pass.TypesInfo, recv)
	if !ok {
		return nil, nil, flow.Key{}, "", false
	}
	if pooledReturn(pass, ctx, s, k) {
		return v, recv, k, method, true
	}
	if freshSilent(pass, ctx, s, k, method) {
		return nil, nil, flow.Key{}, "", false
	}
	if viewSilent(pass, ctx, s, k, method) {
		return nil, nil, flow.Key{}, "", false
	}
	return v, recv, k, method, true
}

func pooledReturn(pass *analysis.Pass, ctx loopCtx, s viewSink, k flow.Key) bool {
	if !s.ret {
		return false
	}
	_, pooled := deferredPoolPut(pass, ctx.fn, k)
	return pooled
}

func freshSilent(pass *analysis.Pass, ctx loopCtx, s viewSink, k flow.Key, viewMethod string) bool {
	return freshReceiver(pass, ctx, k) && !freshInvalidAfter(pass, ctx, s.expr, k, viewMethod)
}

type poolNeed struct {
	s    viewSink
	view ast.Expr
	recv ast.Expr
	key  flow.Key
}

func tryPoolReport(pass *analysis.Pass, ctx loopCtx, need poolNeed, seen map[token.Pos]bool) bool {
	if !need.s.ret {
		return false
	}
	pool, pooled := deferredPoolPut(pass, ctx.fn, need.key)
	if !pooled {
		return false
	}
	if seen[need.s.expr.Pos()] {
		return true
	}
	seen[need.s.expr.Pos()] = true
	pass.Reportf(need.s.expr.Pos(), "%s points into the buffer of %s, and the deferred Put of %s hands it to the pool where another goroutine may overwrite it: store a copy made with bytes.Clone", types.ExprString(need.view), types.ExprString(need.recv), types.ExprString(pool))
	return true
}

func viewSilent(pass *analysis.Pass, ctx loopCtx, sink viewSink, recvKey flow.Key, viewMethod string) bool {
	if sink.ret {
		return returnSilent(pass, ctx.fn, recvKey)
	}
	if sink.assign != nil {
		if v, ok := outerViewVar(pass, ctx.loop, sink.assign, sink.expr); ok {
			return !outerAssignLive(pass, ctx, sink.assign, v, recvKey, viewMethod)
		}
		return !invalidAfter(pass, ctx, sink.assign, recvKey, viewMethod)
	}
	return !invalidAfter(pass, ctx, sink.expr, recvKey, viewMethod)
}

func returnSilent(pass *analysis.Pass, fn ast.Node, recvKey flow.Key) bool {
	_, pooled := deferredPoolPut(pass, fn, recvKey)
	return !pooled
}

func invalidAfter(pass *analysis.Pass, ctx loopCtx, from ast.Node, recvKey flow.Key, viewMethod string) bool {
	g := viewCachedGraph(pass, ctx)
	if g == nil {
		return false
	}
	avoid := assignAvoid(pass, recvKey)
	for _, k := range nearestCalls(viewCachedInvalid(pass, ctx, recvKey, viewMethod), from.Pos()) {
		if g.Reaches(from, k, avoid) {
			return true
		}
	}
	return false
}

func assignAvoid(pass *analysis.Pass, recvKey flow.Key) func(ast.Node) bool {
	v, ok := recvKey.Root.(*types.Var)
	if !ok {
		return nil
	}
	return func(n ast.Node) bool {
		return assignsRoot(pass, n, v)
	}
}

func assignsRoot(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return lhsAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return incAssigns(pass, x.X, v)
	case *ast.RangeStmt:
		return viewRangeBinds(pass, x, v)
	}
	return false
}

func reassignAvoid(pass *analysis.Pass, recvKey flow.Key) func(ast.Node) bool {
	v, ok := recvKey.Root.(*types.Var)
	if !ok {
		return nil
	}
	return func(n ast.Node) bool {
		return reassignsRoot(pass, n, v)
	}
}

func reassignsRoot(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	if declaresVar(pass, n, v) {
		return true
	}
	return assignsRoot(pass, n, v)
}

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

func invalidReceivers(pass *analysis.Pass, loop ast.Node) map[flow.Key][]string {
	out := map[flow.Key][]string{}
	for _, part := range invalidParts(loop) {
		invalidCollect(pass, part, out)
	}
	return out
}

func invalidCollect(pass *analysis.Pass, part ast.Node, out map[flow.Key][]string) {
	ast.Inspect(part, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		invalidAdd(pass, call, out)
		return true
	})
}

func invalidAdd(pass *analysis.Pass, call *ast.CallExpr, out map[flow.Key][]string) {
	name := viewInvalidName(pass, call)
	if name == "" {
		return
	}
	k, ok := flow.MethodKey(pass.TypesInfo, call)
	if !ok {
		return
	}
	if !slices.Contains(out[k], name) {
		out[k] = append(out[k], name)
	}
}

func invalidParts(loop ast.Node) []ast.Node {
	switch x := loop.(type) {
	case *ast.ForStmt:
		parts := []ast.Node{}
		if x.Cond != nil {
			parts = append(parts, x.Cond)
		}
		if x.Body != nil {
			parts = append(parts, x.Body)
		}
		if x.Post != nil {
			parts = append(parts, x.Post)
		}
		return parts
	case *ast.RangeStmt:
		parts := []ast.Node{}
		if x.X != nil {
			parts = append(parts, x.X)
		}
		if x.Body != nil {
			parts = append(parts, x.Body)
		}
		return parts
	}
	return []ast.Node{loop}
}

func viewInvalidName(pass *analysis.Pass, call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || !invalidMethod(sel.Sel.Name) {
		return ""
	}
	obj := typeutil.StaticCallee(pass.TypesInfo, call)
	if obj == nil {
		return ""
	}
	recv := obj.Signature().Recv()
	if recv == nil {
		return ""
	}
	if !viewInvalidRecv(recv.Type(), sel.Sel.Name) {
		return ""
	}
	return sel.Sel.Name
}

func viewInvalidRecv(t types.Type, name string) bool {
	named, ok := derefNamed(t)
	if !ok {
		return false
	}
	path, recv, ok := pkgRecv(named)
	if !ok {
		return false
	}
	for _, want := range viewInvalidRecvTable(name) {
		if path == want.path && recv == want.recv {
			return true
		}
	}
	return false
}

type viewRecv struct {
	path string
	recv string
}

func viewInvalidRecvTable(name string) []viewRecv {
	scanner := viewRecv{viewBufioPath, viewScannerName}
	reader := viewRecv{viewBufioPath, viewReaderName}
	buffer := viewRecv{viewBytesPath, viewBufferName}
	switch name {
	case viewScanName:
		return []viewRecv{scanner}
	case viewResetName:
		return []viewRecv{buffer}
	case closeMethodWrite, viewWriteStringName:
		return []viewRecv{buffer}
	case closeMethodRead:
		return []viewRecv{reader}
	case viewReadSliceName, viewPeekName, viewDiscardName:
		return []viewRecv{reader}
	}
	return nil
}

func invalidMethod(name string) bool {
	switch name {
	case viewScanName, viewResetName, closeMethodWrite, viewWriteStringName, closeMethodRead, viewReadSliceName, viewPeekName, viewDiscardName:
		return true
	}
	return false
}

func derefNamed(t types.Type) (*types.Named, bool) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	return named, ok
}

func pkgRecv(named *types.Named) (path, recv string, ok bool) {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return "", "", false
	}
	return obj.Pkg().Path(), obj.Name(), true
}

func matchViewSource(path, recv, name string) bool {
	switch name {
	case viewBytesName:
		return isBufioScanner(path, recv) || (path == viewBytesPath && recv == viewBufferName)
	case viewPeekName, viewReadSliceName:
		return isBufioReader(path, recv)
	}
	return false
}

func isBufioScanner(path, recv string) bool {
	return path == viewBufioPath && recv == viewScannerName
}

func isBufioReader(path, recv string) bool {
	return path == viewBufioPath && recv == viewReaderName
}

type viewSrc struct {
	recv   ast.Expr
	method string
}

func viewDefs(pass *analysis.Pass, fn ast.Node) map[*types.Var]viewSrc {
	out := map[*types.Var]viewSrc{}
	defPos := map[*types.Var]token.Pos{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || (as.Tok != token.DEFINE && as.Tok != token.ASSIGN) {
			return true
		}
		viewDefineAdd(pass, out, defPos, as)
		return true
	})
	for v, pos := range defPos {
		if viewReassigned(pass, fn, v, pos) {
			delete(out, v)
		}
	}
	return out
}

func viewDefineAdd(pass *analysis.Pass, out map[*types.Var]viewSrc, defPos map[*types.Var]token.Pos, as *ast.AssignStmt) {
	if len(as.Lhs) == 1 && len(as.Rhs) == 1 {
		viewDefineOne(pass, out, defPos, as.Lhs[0], as.Rhs[0], as.Pos())
		return
	}
	if len(as.Rhs) == 1 && len(as.Lhs) == 2 {
		viewDefineOne(pass, out, defPos, as.Lhs[0], as.Rhs[0], as.Pos())
	}
}

func viewDefineOne(pass *analysis.Pass, out map[*types.Var]viewSrc, defPos map[*types.Var]token.Pos, lhs, rhs ast.Expr, pos token.Pos) {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.Defs[id].(*types.Var)
	if !ok {
		v, ok = pass.TypesInfo.Uses[id].(*types.Var)
		if !ok {
			return
		}
	}
	recv, method, ok := viewCallOf(pass, rhs)
	if !ok {
		return
	}
	out[v] = viewSrc{recv: recv, method: method}
	defPos[v] = pos
}

func viewReassigned(pass *analysis.Pass, fn ast.Node, v *types.Var, after token.Pos) bool {
	if litReassignsView(pass, fn, v) {
		return true
	}
	reassigned := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if reassigned {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		if viewReassignsAt(pass, n, v, after) {
			reassigned = true
		}
		return !reassigned
	})
	return reassigned
}

func litReassignsView(pass *analysis.Pass, fn ast.Node, v *types.Var) bool {
	assigned := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if assigned {
			return false
		}
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit == fn {
			return true
		}
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			if assigned {
				return false
			}
			if viewAssignsNode(pass, m, v) {
				assigned = true
				return false
			}
			return true
		})
		return !assigned
	})
	return assigned
}

func viewAssignsNode(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return viewAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return viewTargetIs(pass, x.X, v)
	}
	return false
}

func viewReassignsAt(pass *analysis.Pass, n ast.Node, v *types.Var, after token.Pos) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return x.Pos() > after && viewAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return x.Pos() > after && viewTargetIs(pass, x.X, v)
	}
	return false
}

func viewRangeBinds(pass *analysis.Pass, x *ast.RangeStmt, v *types.Var) bool {
	for _, e := range []ast.Expr{x.Key, x.Value} {
		if viewTargetIs(pass, e, v) {
			return true
		}
	}
	return false
}

func viewAssigns(pass *analysis.Pass, lhs []ast.Expr, v *types.Var) bool {
	for _, l := range lhs {
		if viewTargetIs(pass, l, v) {
			return true
		}
	}
	return false
}

func viewTargetIs(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	return pass.TypesInfo.Uses[id] == v || pass.TypesInfo.Defs[id] == v
}

func viewCallOf(pass *analysis.Pass, e ast.Expr) (ast.Expr, string, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	return viewCallRecv(pass, call)
}

func viewCallRecv(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, string, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, "", false
	}
	switch sel.Sel.Name {
	case viewBytesName, viewPeekName, viewReadSliceName:
	default:
		return nil, "", false
	}
	obj := typeutil.StaticCallee(pass.TypesInfo, call)
	if obj == nil {
		return nil, "", false
	}
	recv := obj.Signature().Recv()
	if recv == nil {
		return nil, "", false
	}
	if !isViewSourceType(recv.Type(), sel.Sel.Name) {
		return nil, "", false
	}
	return sel.X, sel.Sel.Name, true
}

func isViewSourceType(t types.Type, name string) bool {
	named, ok := derefNamed(t)
	if !ok {
		return false
	}
	path, recv, ok := pkgRecv(named)
	if !ok {
		return false
	}
	return matchViewSource(path, recv, name)
}

type loopCtx struct {
	fn    ast.Node
	loop  ast.Node
	body  ast.Node
	cache *viewCache
}

func viewCachedGraph(pass *analysis.Pass, ctx loopCtx) *flow.Graph {
	if ctx.cache == nil {
		return flow.NewGraph(pass, ctx.fn)
	}
	if ctx.cache.graphDone {
		return ctx.cache.graph
	}
	ctx.cache.graph = flow.NewGraph(pass, ctx.fn)
	ctx.cache.graphDone = true
	return ctx.cache.graph
}

func viewCachedInvalid(pass *analysis.Pass, ctx loopCtx, recvKey flow.Key, viewMethod string) []*ast.CallExpr {
	if ctx.cache == nil {
		return invalidCalls(pass, ctx.fn, recvKey, viewMethod)
	}
	k := invalidKey{recv: recvKey, method: viewMethod}
	if out, ok := ctx.cache.invalid[k]; ok {
		return out
	}
	out := invalidCalls(pass, ctx.fn, recvKey, viewMethod)
	ctx.cache.invalid[k] = out
	return out
}

func viewRangeSet(ctx loopCtx) map[*ast.Ident]bool {
	if ctx.cache != nil && ctx.cache.rangeDone {
		return ctx.cache.rangeSet
	}
	out := map[*ast.Ident]bool{}
	ast.Inspect(ctx.fn, func(n ast.Node) bool {
		r, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if id, ok := ast.Unparen(r.Key).(*ast.Ident); ok {
			out[id] = true
		}
		if id, ok := ast.Unparen(r.Value).(*ast.Ident); ok {
			out[id] = true
		}
		return true
	})
	if ctx.cache != nil {
		ctx.cache.rangeSet = out
		ctx.cache.rangeDone = true
	}
	return out
}

func viewAssignsFast(pass *analysis.Pass, ctx loopCtx, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return lhsAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return incAssigns(pass, x.X, v)
	case *ast.Ident:
		if pass.TypesInfo.Uses[x] != v {
			return false
		}
		return viewRangeSet(ctx)[x]
	case *ast.ValueSpec:
		return false
	}
	return false
}

func viewAssignList(pass *analysis.Pass, ctx loopCtx, v *types.Var) []*ast.AssignStmt {
	if ctx.cache != nil {
		if out, ok := ctx.cache.assignLists[v]; ok {
			return out
		}
	}
	var out []*ast.AssignStmt
	ast.Inspect(ctx.fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != ctx.fn {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if lhsAssigns(pass, as.Lhs, v) {
			out = append(out, as)
		}
		return true
	})
	if ctx.cache != nil {
		ctx.cache.assignLists[v] = out
	}
	return out
}

func viewCallInAssign(pass *analysis.Pass, ctx loopCtx, call *ast.CallExpr, v *types.Var) bool {
	for _, as := range viewAssignList(pass, ctx, v) {
		if as.Pos() <= call.Pos() && call.End() <= as.End() {
			return true
		}
	}
	return false
}

func viewCachedLHS(pass *analysis.Pass, ctx loopCtx, v *types.Var) map[*ast.Ident]bool {
	if ctx.cache != nil {
		if out, ok := ctx.cache.lhsSets[v]; ok {
			return out
		}
	}
	out := assignLHSIdents(pass, ctx.fn, v)
	if ctx.cache != nil {
		ctx.cache.lhsSets[v] = out
	}
	return out
}

func viewCachedDiscard(pass *analysis.Pass, ctx loopCtx, v *types.Var) map[*ast.Ident]bool {
	if ctx.cache != nil {
		if out, ok := ctx.cache.discardSets[v]; ok {
			return out
		}
	}
	out := discardRHSIdents(pass, ctx.fn, v)
	if ctx.cache != nil {
		ctx.cache.discardSets[v] = out
	}
	return out
}

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
