package resstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

type invalidKey struct {
	recv   flow.Key
	method string
}

type callInKey struct {
	k *ast.CallExpr
	v *types.Var
}

type viewCache struct {
	graph       *flow.Graph
	graphDone   bool
	invalid     map[invalidKey][]*ast.CallExpr
	rangeSet    map[*ast.Ident]bool
	rangeDone   bool
	assignLists map[*types.Var][]*ast.AssignStmt
	lhsSets     map[*types.Var]map[*ast.Ident]bool
	discardSets map[*types.Var]map[*ast.Ident]bool
	callIn      map[callInKey]bool
	hasReads    map[*types.Var]bool
	readsDone   map[*types.Var]bool
}

func newViewCache() *viewCache {
	return &viewCache{
		invalid:     map[invalidKey][]*ast.CallExpr{},
		assignLists: map[*types.Var][]*ast.AssignStmt{},
		lhsSets:     map[*types.Var]map[*ast.Ident]bool{},
		discardSets: map[*types.Var]map[*ast.Ident]bool{},
		callIn:      map[callInKey]bool{},
		hasReads:    map[*types.Var]bool{},
		readsDone:   map[*types.Var]bool{},
	}
}

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
	if !viewHasReads(pass, ctx, v) {
		return false
	}
	avoid := func(n ast.Node) bool {
		return viewAssignsFast(pass, ctx, n, v)
	}
	for _, k := range nearestCalls(calls, sink.Pos()) {
		if viewCachedCallInAssign(pass, ctx, k, v) {
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

func viewCachedCallInAssign(pass *analysis.Pass, ctx loopCtx, k *ast.CallExpr, v *types.Var) bool {
	if ctx.cache != nil {
		if out, ok := ctx.cache.callIn[callInKey{k: k, v: v}]; ok {
			return out
		}
	}
	out := viewCallInAssign(pass, ctx, k, v)
	if ctx.cache != nil {
		ctx.cache.callIn[callInKey{k: k, v: v}] = out
	}
	return out
}

func viewHasReads(pass *analysis.Pass, ctx loopCtx, v *types.Var) bool {
	if ctx.cache != nil && ctx.cache.readsDone[v] {
		return ctx.cache.hasReads[v]
	}
	if hasReturnRead(pass, ctx, v) {
		storeReads(ctx, v, true)
		return true
	}
	out := hasIdentRead(pass, ctx, v)
	storeReads(ctx, v, out)
	return out
}

func storeReads(ctx loopCtx, v *types.Var, out bool) {
	if ctx.cache == nil {
		return
	}
	ctx.cache.readsDone[v] = true
	ctx.cache.hasReads[v] = out
}

func hasReturnRead(pass *analysis.Pass, ctx loopCtx, v *types.Var) bool {
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
		found = returnReads(pass, ret, v, ctx.fn)
		return !found
	})
	return found
}

func hasIdentRead(pass *analysis.Pass, ctx loopCtx, v *types.Var) bool {
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
		if isLiveUse(pass, id, v, skip, discard) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isLiveUse(pass *analysis.Pass, id *ast.Ident, v *types.Var, skip, discard map[*ast.Ident]bool) bool {
	return pass.TypesInfo.Uses[id] == v && !skip[id] && !discard[id]
}

func nearestCalls(calls []*ast.CallExpr, pos token.Pos) []*ast.CallExpr {
	lo := 0
	hi := len(calls)
	for lo < hi {
		mid := (lo + hi) / 2
		if calls[mid].Pos() < pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	out := make([]*ast.CallExpr, 0, len(calls))
	i := lo - 1
	j := lo
	for len(out) < len(calls) {
		if i < 0 {
			out = append(out, calls[j])
			j++
			continue
		}
		if j >= len(calls) {
			out = append(out, calls[i])
			i--
			continue
		}
		if pos-calls[i].Pos() < calls[j].Pos()-pos {
			out = append(out, calls[i])
			i--
		} else {
			out = append(out, calls[j])
			j++
		}
	}
	return out
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
	if _, ok := invalidName(pass, call, viewMethod); !ok {
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

func shadowedByRange(pass *analysis.Pass, fn ast.Node, sink, view ast.Expr, views map[*types.Var]viewSrc) bool {
	id, ok := ast.Unparen(view).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return false
	}
	src, ok := views[v]
	if !ok {
		return false
	}
	if !inAssigningRange(pass, fn, sink.Pos(), v) {
		return false
	}
	return rangeHidesView(pass, fn, sink, v, src, views)
}

func rangeHidesView(pass *analysis.Pass, fn ast.Node, sink ast.Expr, v *types.Var, src viewSrc, views map[*types.Var]viewSrc) bool {
	assign, assignView, assignPos := latestAssignBefore(pass, fn, sink.Pos(), v)
	rng, rangePos := latestRangeBefore(pass, fn, sink.Pos(), v)
	if assign != nil && assignPos > rangePos {
		return !assignView
	}
	if rng == nil || rng.X == nil {
		return true
	}
	if exprAliasesView(pass, rng.X, newAliasScope(pass, v, src, views)) {
		return false
	}
	return true
}

func latestAssignBefore(pass *analysis.Pass, fn ast.Node, pos token.Pos, v *types.Var) (*ast.AssignStmt, bool, token.Pos) {
	var best *ast.AssignStmt
	bestView := false
	bestPos := token.NoPos
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		x, ok := n.(*ast.AssignStmt)
		if !ok || x.Pos() >= pos || !viewAssigns(pass, x.Lhs, v) || x.Pos() <= bestPos {
			return true
		}
		best, bestPos = x, x.Pos()
		bestView = isViewDefAssign(pass, x, v)
		return true
	})
	return best, bestView, bestPos
}

func latestRangeBefore(pass *analysis.Pass, fn ast.Node, pos token.Pos, v *types.Var) (*ast.RangeStmt, token.Pos) {
	var best *ast.RangeStmt
	bestPos := token.NoPos
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		x, ok := n.(*ast.RangeStmt)
		if !ok || x.Pos() >= pos || !rangeAssigns(pass, x, v) || x.Pos() <= bestPos {
			return true
		}
		best, bestPos = x, x.Pos()
		return true
	})
	return best, bestPos
}

func isViewDefAssign(pass *analysis.Pass, as *ast.AssignStmt, v *types.Var) bool {
	if !viewAssigns(pass, as.Lhs, v) || len(as.Rhs) != 1 {
		return false
	}
	_, _, ok := viewCallOf(pass, as.Rhs[0])
	return ok
}

type aliasScope struct {
	v      *types.Var
	recv   flow.Key
	recvOK bool
	views  map[*types.Var]viewSrc
}

func newAliasScope(pass *analysis.Pass, v *types.Var, src viewSrc, views map[*types.Var]viewSrc) aliasScope {
	k, ok := flow.KeyOf(pass.TypesInfo, src.recv)
	return aliasScope{v: v, recv: k, recvOK: ok, views: views}
}

func exprAliasesView(pass *analysis.Pass, e ast.Expr, s aliasScope) bool {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		return aliasIdent(pass, id, s)
	}
	if se, ok := e.(*ast.SliceExpr); ok {
		return aliasSlice(pass, se, s)
	}
	if lit, ok := e.(*ast.CompositeLit); ok {
		return aliasLit(pass, lit, s)
	}
	if call, ok := e.(*ast.CallExpr); ok {
		return aliasCall(pass, call, s)
	}
	return false
}

func aliasIdent(pass *analysis.Pass, id *ast.Ident, s aliasScope) bool {
	if pass.TypesInfo.Uses[id] == s.v {
		return true
	}
	w, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return false
	}
	other, ok := s.views[w]
	if !ok || !s.recvOK {
		return false
	}
	k, ok := flow.KeyOf(pass.TypesInfo, other.recv)
	if !ok {
		return false
	}
	return k == s.recv
}

func aliasSlice(pass *analysis.Pass, se *ast.SliceExpr, s aliasScope) bool {
	if se.X == nil {
		return false
	}
	return exprAliasesView(pass, se.X, s)
}

func aliasLit(pass *analysis.Pass, lit *ast.CompositeLit, s aliasScope) bool {
	for _, elt := range lit.Elts {
		if exprAliasesView(pass, elt, s) {
			return true
		}
	}
	return false
}

func aliasCall(pass *analysis.Pass, call *ast.CallExpr, s aliasScope) bool {
	if aliasViewCall(pass, call, s) {
		return true
	}
	if isAppendCall(pass, call) {
		if call.Ellipsis.IsValid() {
			return false
		}
		return aliasAnyArg(pass, appendElemArgs(call, nil), s)
	}
	if !isBytesAliasingCall(pass, call) {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	return exprAliasesView(pass, call.Args[0], s)
}

func aliasViewCall(pass *analysis.Pass, call *ast.CallExpr, s aliasScope) bool {
	recv, _, ok := viewCallRecv(pass, call)
	if !ok || !s.recvOK {
		return false
	}
	k, ok := flow.KeyOf(pass.TypesInfo, recv)
	if !ok {
		return false
	}
	return k == s.recv
}

func aliasAnyArg(pass *analysis.Pass, args []ast.Expr, s aliasScope) bool {
	for _, arg := range args {
		if exprAliasesView(pass, arg, s) {
			return true
		}
	}
	return false
}

func isBytesAliasingCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != viewBytesPath {
		return false
	}
	return bytesAliasingFuncs[fn.Name()]
}

var bytesAliasingFuncs = map[string]bool{
	"Split": true, "SplitN": true, "SplitAfter": true, "SplitAfterN": true,
	"SplitSeq": true, "SplitAfterSeq": true,
	"Fields": true, "FieldsFunc": true, "FieldsSeq": true, "FieldsFuncSeq": true,
	"Lines": true,
	"Trim":  true, "TrimLeft": true, "TrimRight": true, "TrimSpace": true,
	"TrimFunc": true, "TrimLeftFunc": true, "TrimRightFunc": true,
	"TrimPrefix": true, "TrimSuffix": true,
	"Cut": true, "CutPrefix": true, "CutSuffix": true, "CutLast": true,
}

func inAssigningRange(pass *analysis.Pass, fn ast.Node, pos token.Pos, v *types.Var) bool {
	shadowed := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if shadowed {
			return false
		}
		r, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if !rangeAssigns(pass, r, v) {
			return true
		}
		if r.Body != nil && pos >= r.Body.Pos() && pos < r.Body.End() {
			shadowed = true
			return false
		}
		return true
	})
	return shadowed
}

func rangeAssigns(pass *analysis.Pass, r *ast.RangeStmt, v *types.Var) bool {
	if r.Key != nil && viewTargetIs(pass, r.Key, v) {
		return true
	}
	return r.Value != nil && viewTargetIs(pass, r.Value, v)
}
