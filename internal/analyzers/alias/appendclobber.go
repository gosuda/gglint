package alias

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func runAppendclobber(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkAppendclobber(pass, fn)
	})
	return nil, nil
}

type sliceDef struct {
	a         *types.Var
	pos       token.Pos
	high      int
	highKnown bool
	zeroLow   bool
}

func checkAppendclobber(pass *analysis.Pass, fn ast.Node) {
	defs := collectSliceDefs(pass.TypesInfo, fn)
	if len(defs) == 0 {
		return
	}
	silent := map[*types.Var]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok {
			checkAppendAssign(pass, fn, st, defs, silent)
		}
		return true
	})
}

func collectSliceDefs(info *types.Info, fn ast.Node) map[*types.Var]sliceDef {
	defs := map[*types.Var]sliceDef{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok {
			addSliceAssign(info, fn, st, defs)
		}
		return true
	})
	return defs
}

func addSliceAssign(info *types.Info, fn ast.Node, st *ast.AssignStmt, defs map[*types.Var]sliceDef) {
	if st.Tok != token.DEFINE || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return
	}
	b, ok := identVar(info, st.Lhs[0])
	if !ok || !flow.IsLocal(info, fn, b) {
		return
	}
	addSliceExpr(info, fn, st.Rhs[0], b, st.Pos(), defs)
}

func addSliceExpr(info *types.Info, fn ast.Node, e ast.Expr, b *types.Var, pos token.Pos, defs map[*types.Var]sliceDef) {
	s, ok := ast.Unparen(e).(*ast.SliceExpr)
	if !ok || s.Max != nil || s.High == nil {
		return
	}
	a, ok := identVar(info, s.X)
	if !ok || !flow.IsLocal(info, fn, a) {
		return
	}
	if isLenOf(info, s.High, a) {
		return
	}
	high, highKnown := constInt(info, s.High)
	zeroLow := sliceLowZero(info, s.Low)
	defs[b] = sliceDef{a: a, pos: pos, high: high, highKnown: highKnown, zeroLow: zeroLow}
}

func sliceLowZero(info *types.Info, low ast.Expr) bool {
	if low == nil {
		return true
	}
	v, ok := constInt(info, low)
	return ok && v == 0
}

func constInt(info *types.Info, e ast.Expr) (int, bool) {
	tv, ok := info.Types[ast.Unparen(e)]
	if !ok || tv.Value == nil {
		return 0, false
	}
	v, ok := constant.Int64Val(tv.Value)
	if !ok {
		return 0, false
	}
	return int(v), true
}

func isLenOf(info *types.Info, e ast.Expr, target *types.Var) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	if !ok || b.Name() != "len" {
		return false
	}
	v, ok := identVar(info, call.Args[0])
	return ok && v == target
}

func checkAppendAssign(pass *analysis.Pass, fn ast.Node, st *ast.AssignStmt, defs map[*types.Var]sliceDef, silent map[*types.Var]bool) {
	b, def, call, ok := appendDef(pass.TypesInfo, st, defs)
	if !ok {
		return
	}
	if !def.highKnown || call.Ellipsis.IsValid() {
		return
	}
	if inPlaceCached(pass, fn, b, def, silent) {
		return
	}
	if assignedBetween(pass.TypesInfo, fn, b, def.pos, st.Pos()) {
		return
	}
	if assignedBetween(pass.TypesInfo, fn, def.a, def.pos, st.Pos()) {
		return
	}
	if tryKnownCap(pass, fn, st, b, def, call) {
		return
	}
	tryUnknownCap(pass, fn, st, b, def, call)
}

func inPlaceCached(pass *analysis.Pass, fn ast.Node, b *types.Var, def sliceDef, silent map[*types.Var]bool) bool {
	if v, ok := silent[b]; ok {
		return v
	}
	v := inPlaceSilent(pass, fn, b, def)
	silent[b] = v
	return v
}

func tryKnownCap(pass *analysis.Pass, fn ast.Node, st *ast.AssignStmt, b *types.Var, def sliceDef, call *ast.CallExpr) bool {
	need := def.high + len(call.Args) - 1
	caps := reachingCaps(pass, fn, def.a, def.pos)
	if len(caps) == 0 {
		return false
	}
	if !allKnown(caps) {
		return false
	}
	for _, c := range caps {
		if c.cap < need {
			continue
		}
		if !knownCapProven(pass, fn, def, st, len(call.Args)-1) {
			return true
		}
		reportClobber(pass, call, b, def.a)
		return true
	}
	return true
}

func allKnown(caps []capResult) bool {
	for _, c := range caps {
		if !c.known {
			return false
		}
	}
	return true
}

func knownCapProven(pass *analysis.Pass, fn ast.Node, def sliceDef, st *ast.AssignStmt, appended int) bool {
	return overwrittenReadAfter(pass, fn, def.a, readSite{from: st, after: st.End(), bound: def.high, end: clobberEnd(fn, st, def.high, appended)})
}

func clobberEnd(fn ast.Node, st *ast.AssignStmt, bound, appended int) int {
	if enclosingLoop(fn, st) != nil {
		return 1 << 30
	}
	return bound + appended
}

type capResult struct {
	cap   int
	known bool
}

func reachingCaps(pass *analysis.Pass, fn ast.Node, a *types.Var, slicePos token.Pos) []capResult {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return nil
	}
	to := findAssignAt(fn, slicePos)
	if to == nil {
		return nil
	}
	var out []capResult
	for _, from := range collectAssignsTo(pass.TypesInfo, fn, a) {
		if !reachesForCap(g, pass.TypesInfo, a, from, to) {
			continue
		}
		c, ok := capOfAssignment(pass.TypesInfo, from, a)
		out = append(out, capResult{cap: c, known: ok})
	}
	return out
}

func findAssignAt(fn ast.Node, pos token.Pos) ast.Node {
	var found ast.Node
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok && st.Pos() == pos {
			found = st
			return false
		}
		return true
	})
	return found
}

func collectAssignsTo(info *types.Info, fn ast.Node, a *types.Var) []*ast.AssignStmt {
	var out []*ast.AssignStmt
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok && assignsVar(info, st, a) {
			out = append(out, st)
		}
		return true
	})
	return out
}

func reachesForCap(g *flow.Graph, info *types.Info, a *types.Var, from, to ast.Node) bool {
	avoid := func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		return ok && assignsVar(info, st, a)
	}
	return g.Reaches(from, to, avoid)
}

func capOfAssignment(info *types.Info, st *ast.AssignStmt, a *types.Var) (int, bool) {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return 0, false
	}
	if !isVarTarget(info, st.Lhs[0], a) {
		return 0, false
	}
	rhs := ast.Unparen(st.Rhs[0])
	if _, ok := rhs.(*ast.CompositeLit); ok {
		return defLenCap(info, st.Rhs[0])
	}
	if call, ok := rhs.(*ast.CallExpr); ok && isMake(info, call) {
		return defLenCap(info, st.Rhs[0])
	}
	return threeIndexCap(info, st, a)
}

func tryUnknownCap(pass *analysis.Pass, fn ast.Node, st *ast.AssignStmt, b *types.Var, def sliceDef, call *ast.CallExpr) {
	n := len(call.Args) - 1
	end := clobberEnd(fn, st, def.high, n)
	if leng, ok := provenLen(pass.TypesInfo, fn, def.a, def.pos); ok && def.high+n <= leng {
		if overwrittenReadAfter(pass, fn, def.a, readSite{from: st, after: st.End(), bound: def.high, end: end}) {
			reportClobber(pass, call, b, def.a)
		}
		return
	}
	if !hasCoveringIndex(pass, fn, def.a, st, def.high+n-1) {
		return
	}
	if overwrittenReadAfter(pass, fn, def.a, readSite{from: st, after: st.End(), bound: def.high, end: end}) {
		reportClobber(pass, call, b, def.a)
	}
}

func reportClobber(pass *analysis.Pass, call *ast.CallExpr, b, a *types.Var) {
	pass.Reportf(call.Pos(), "%s shares its backing array with %s, so this append overwrites elements of %s that are read later: slice it with three indexes to cap its capacity", b.Name(), a.Name(), a.Name())
}

func appendDef(info *types.Info, st *ast.AssignStmt, defs map[*types.Var]sliceDef) (*types.Var, sliceDef, *ast.CallExpr, bool) {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return nil, sliceDef{}, nil, false
	}
	call, ok := isAppend(info, st.Rhs[0])
	if !ok || len(call.Args) < 2 {
		return nil, sliceDef{}, nil, false
	}
	b, ok := appendTarget(info, st, call)
	if !ok {
		return nil, sliceDef{}, nil, false
	}
	def, ok := defs[b]
	if !ok {
		return nil, sliceDef{}, nil, false
	}
	return b, def, call, true
}

func threeIndexCap(info *types.Info, st *ast.AssignStmt, a *types.Var) (int, bool) {
	s, ok := threeSlice(info, st, a)
	if !ok {
		return 0, false
	}
	low, ok := sliceLowBound(info, s)
	if !ok {
		return 0, false
	}
	mx, ok := constInt(info, s.Max)
	if !ok || mx < low {
		return 0, false
	}
	return mx - low, true
}

func threeSlice(info *types.Info, st *ast.AssignStmt, a *types.Var) (*ast.SliceExpr, bool) {
	if st.Tok != token.ASSIGN || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return nil, false
	}
	if !isVarTarget(info, st.Lhs[0], a) {
		return nil, false
	}
	s, ok := ast.Unparen(st.Rhs[0]).(*ast.SliceExpr)
	if !ok || s.Max == nil {
		return nil, false
	}
	xa, ok := identVar(info, s.X)
	if !ok || xa != a {
		return nil, false
	}
	return s, true
}

func sliceLowBound(info *types.Info, s *ast.SliceExpr) (int, bool) {
	if s.Low == nil {
		return 0, true
	}
	return constInt(info, s.Low)
}

type lenTrack struct {
	cur   int
	known bool
	seen  bool
}

func provenLen(info *types.Info, fn ast.Node, a *types.Var, before token.Pos) (int, bool) {
	tr := &lenTrack{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		return visitLenNode(info, n, a, before, tr)
	})
	if !tr.seen {
		return 0, false
	}
	return tr.cur, tr.known
}

func visitLenNode(info *types.Info, n ast.Node, a *types.Var, before token.Pos, tr *lenTrack) bool {
	if _, ok := n.(*ast.FuncLit); ok {
		return false
	}
	st, ok := n.(*ast.AssignStmt)
	if !ok || st.Pos() >= before || !assignsVar(info, st, a) {
		return true
	}
	applyLenStmt(info, st, a, tr)
	return true
}

func applyLenStmt(info *types.Info, st *ast.AssignStmt, a *types.Var, tr *lenTrack) {
	if isDefOf(info, st, a) {
		l, ok := defLenValue(info, st.Rhs[0])
		tr.cur, tr.known, tr.seen = l, ok, true
		return
	}
	if !tr.seen {
		tr.seen = true
		tr.known = false
		return
	}
	if !tr.known {
		return
	}
	next, ok := lenAfterAssign(info, st, a, tr.cur)
	if !ok {
		tr.known = false
		return
	}
	tr.cur = next
}

func lenAfterAssign(info *types.Info, st *ast.AssignStmt, a *types.Var, cur int) (int, bool) {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return 0, false
	}
	if call, ok := isAppend(info, st.Rhs[0]); ok {
		return appendLenGrowth(info, call, a, cur)
	}
	if l, ok := defLenValue(info, st.Rhs[0]); ok && isVarTarget(info, st.Lhs[0], a) {
		if _, ok := ast.Unparen(st.Rhs[0]).(*ast.CallExpr); ok {
			return l, true
		}
	}
	if c, ok := threeIndexLen(info, st, a); ok {
		return c, true
	}
	return 0, false
}

func appendLenGrowth(info *types.Info, call *ast.CallExpr, a *types.Var, cur int) (int, bool) {
	if call.Ellipsis.IsValid() || len(call.Args) < 2 {
		return 0, false
	}
	v, ok := identVar(info, call.Args[0])
	if !ok || v != a {
		return 0, false
	}
	return cur + len(call.Args) - 1, true
}

func threeIndexLen(info *types.Info, st *ast.AssignStmt, a *types.Var) (int, bool) {
	s, ok := threeSlice(info, st, a)
	if !ok || s.High == nil {
		return 0, false
	}
	low, ok := sliceLowBound(info, s)
	if !ok {
		return 0, false
	}
	high, ok := constInt(info, s.High)
	if !ok || high < low {
		return 0, false
	}
	return high - low, true
}

func defLenValue(info *types.Info, rhs ast.Expr) (int, bool) {
	if comp, ok := ast.Unparen(rhs).(*ast.CompositeLit); ok {
		return compositeLen(info, comp)
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok || !isMake(info, call) || len(call.Args) < 2 {
		return 0, false
	}
	return constInt(info, call.Args[1])
}

func compositeLen(info *types.Info, comp *ast.CompositeLit) (int, bool) {
	if typ := info.TypeOf(comp); typ != nil {
		if arr, ok := typ.Underlying().(*types.Array); ok {
			return int(arr.Len()), true
		}
	}
	if hasKeyed(comp) {
		return 0, false
	}
	return len(comp.Elts), true
}

func indexOf(info *types.Info, idx *ast.IndexExpr, a *types.Var, threshold int) bool {
	v, ok := identVar(info, idx.X)
	if !ok || v != a {
		return false
	}
	k, ok := constInt(info, idx.Index)
	return ok && k >= threshold
}

func isLenCapCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && (b.Name() == "len" || b.Name() == "cap")
}

func atMostOnePerIteration(pass *analysis.Pass, fn ast.Node, appends []*ast.AssignStmt) bool {
	if len(appends) <= 1 {
		return true
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return false
	}
	for i := range appends {
		for j := i + 1; j < len(appends); j++ {
			from := appends[i]
			to := appends[j]
			if from.Pos() > to.Pos() {
				from, to = to, from
			}
			avoid := func(n ast.Node) bool {
				return n.Pos() < from.Pos() || n.Pos() > to.End()
			}
			if g.Reaches(from, to, avoid) {
				return false
			}
		}
	}
	return true
}

func appendsTo(info *types.Info, fn ast.Node, b *types.Var) []*ast.AssignStmt {
	var out []*ast.AssignStmt
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if ok && isAppendTo(info, st, b) {
			out = append(out, st)
		}
		return true
	})
	return out
}

func isAppendTo(info *types.Info, st *ast.AssignStmt, b *types.Var) bool {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return false
	}
	call, ok := isAppend(info, st.Rhs[0])
	if !ok || len(call.Args) < 2 {
		return false
	}
	v, ok := identVar(info, call.Args[0])
	return ok && v == b
}

func singleInReadingLoop(info *types.Info, fn ast.Node, st *ast.AssignStmt, a *types.Var) bool {
	call, ok := isAppend(info, st.Rhs[0])
	if !ok || call.Ellipsis.IsValid() || len(call.Args) != 2 {
		return false
	}
	loop := enclosingLoop(fn, st)
	if loop == nil {
		return false
	}
	return readsBefore(info, loop, st, a)
}

func enclosingLoop(fn ast.Node, target *ast.AssignStmt) ast.Node {
	var best ast.Node
	bestSize := token.Pos(1 << 30)
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		var body *ast.BlockStmt
		switch t := n.(type) {
		case *ast.ForStmt:
			body = t.Body
		case *ast.RangeStmt:
			body = t.Body
		default:
			return true
		}
		if body == nil || target.Pos() <= body.Pos() || target.End() > body.End() {
			return true
		}
		if sz := body.End() - body.Pos(); sz < bestSize {
			bestSize = sz
			best = n
		}
		return true
	})
	return best
}

func readsBefore(info *types.Info, loop ast.Node, target *ast.AssignStmt, a *types.Var) bool {
	var body *ast.BlockStmt
	var header ast.Expr
	var forLoop *ast.ForStmt
	switch t := loop.(type) {
	case *ast.ForStmt:
		body = t.Body
		forLoop = t
	case *ast.RangeStmt:
		body = t.Body
		header = t.X
	default:
		return false
	}
	if header != nil {
		if v, ok := identVar(info, header); ok && v == a {
			return true
		}
	}
	if hasAdvanceBefore(info, body, target, a) {
		return true
	}
	if forLoop != nil {
		return hasCounterIndexBefore(info, forLoop, body, target, a)
	}
	return false
}

func hasAdvanceBefore(info *types.Info, body *ast.BlockStmt, target *ast.AssignStmt, a *types.Var) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok || st.Pos() >= target.Pos() || !isSliceAdvance(info, st, a) {
			return true
		}
		found = true
		return false
	})
	return found
}

func hasCounterIndexBefore(info *types.Info, loop *ast.ForStmt, body *ast.BlockStmt, target *ast.AssignStmt, a *types.Var) bool {
	counter, ok := loopCounter(info, loop)
	if !ok {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		if counterIndexHit(info, idx, target, a, counter) {
			found = true
		}
		return !found
	})
	return found
}

func counterIndexHit(info *types.Info, idx *ast.IndexExpr, target *ast.AssignStmt, a, counter *types.Var) bool {
	if v, ok := identVar(info, idx.X); !ok || v != a || idx.Pos() >= target.Pos() {
		return false
	}
	id, ok := ast.Unparen(idx.Index).(*ast.Ident)
	if !ok {
		return false
	}
	cv, ok := varOf(info, id)
	return ok && cv == counter
}

func loopCounter(info *types.Info, loop *ast.ForStmt) (*types.Var, bool) {
	if loop.Init == nil {
		return nil, false
	}
	st, ok := loop.Init.(*ast.AssignStmt)
	if !ok || len(st.Lhs) != 1 {
		return nil, false
	}
	return identVar(info, st.Lhs[0])
}

func isSliceAdvance(info *types.Info, st *ast.AssignStmt, a *types.Var) bool {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return false
	}
	if !isVarTarget(info, st.Lhs[0], a) {
		return false
	}
	s, ok := ast.Unparen(st.Rhs[0]).(*ast.SliceExpr)
	if !ok {
		return false
	}
	v, ok := identVar(info, s.X)
	return ok && v == a
}

func assignsVar(info *types.Info, st *ast.AssignStmt, a *types.Var) bool {
	for _, lhs := range st.Lhs {
		if isVarTarget(info, lhs, a) {
			return true
		}
	}
	return false
}

func isDefOf(info *types.Info, st *ast.AssignStmt, a *types.Var) bool {
	if st.Tok != token.DEFINE || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return false
	}
	v, ok := identVar(info, st.Lhs[0])
	return ok && v == a
}

func defLenCap(info *types.Info, rhs ast.Expr) (capacity int, capKnown bool) {
	if comp, ok := ast.Unparen(rhs).(*ast.CompositeLit); ok {
		return compositeCap(info, comp)
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok || !isMake(info, call) || len(call.Args) < 2 {
		return 0, false
	}
	return makeLenCap(info, call)
}

func compositeCap(info *types.Info, comp *ast.CompositeLit) (capacity int, capKnown bool) {
	if typ := info.TypeOf(comp); typ != nil {
		if arr, ok := typ.Underlying().(*types.Array); ok {
			return int(arr.Len()), true
		}
	}
	if hasKeyed(comp) {
		return 0, false
	}
	return len(comp.Elts), true
}

func hasKeyed(comp *ast.CompositeLit) bool {
	for _, e := range comp.Elts {
		if _, ok := ast.Unparen(e).(*ast.KeyValueExpr); ok {
			return true
		}
	}
	return false
}

func isMake(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "make"
}

func makeLenCap(info *types.Info, call *ast.CallExpr) (capacity int, capKnown bool) {
	l, lok := constInt(info, call.Args[1])
	if len(call.Args) < 3 {
		return l, lok
	}
	c, cok := constInt(info, call.Args[2])
	if !lok || !cok {
		return 0, false
	}
	return c, true
}

func appendTarget(info *types.Info, st *ast.AssignStmt, call *ast.CallExpr) (*types.Var, bool) {
	b, ok := identVar(info, call.Args[0])
	if !ok {
		return nil, false
	}
	if st.Tok == token.ASSIGN {
		lhs, ok := identVar(info, st.Lhs[0])
		if !ok || lhs != b {
			return nil, false
		}
		return b, true
	}
	if st.Tok != token.DEFINE {
		return nil, false
	}
	return b, true
}

func assignedBetween(info *types.Info, fn ast.Node, v *types.Var, from, to token.Pos) bool {
	hit := false
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok {
			hit = assignHits(info, st, v, from, to)
		}
		return !hit
	})
	return hit
}

func assignHits(info *types.Info, st *ast.AssignStmt, v *types.Var, from, to token.Pos) bool {
	if st.Pos() <= from || st.Pos() >= to {
		return false
	}
	for _, lhs := range st.Lhs {
		if isVarTarget(info, lhs, v) {
			return true
		}
	}
	return false
}

func isVarTarget(info *types.Info, e ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	got, ok := varOf(info, id)
	return ok && got == v
}
