package resstate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

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
