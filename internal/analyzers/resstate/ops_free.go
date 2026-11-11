package resstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type freeQuery struct {
	pass    *analysis.Pass
	fn      ast.Node
	release ast.Node
	v       *types.Var
	cache   *freeTempCache
}

func freeMunmapByPkg(q freeQuery, pkg *types.Package, call *ast.CallExpr) bool {
	if pkg.Path() != "syscall" && pkg.Path() != "golang.org/x/sys/unix" {
		return false
	}
	return freeArgsMention(q, call)
}

func inPlaceLit(n ast.Node) (*ast.FuncLit, bool) {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
	if !ok || len(call.Args) != 0 || lit.Body == nil {
		return nil, false
	}
	return lit, true
}

func flatLit(lit *ast.FuncLit) bool {
	if lit.Body == nil {
		return false
	}
	for _, s := range lit.Body.List {
		switch s.(type) {
		case *ast.ExprStmt, *ast.AssignStmt, *ast.IncDecStmt, *ast.DeclStmt:
		default:
			return false
		}
	}
	return true
}

type freeTrack struct {
	pass  *analysis.Pass
	fn    ast.Node
	v     *types.Var
	cache *freeTempCache
}

func freeLitState(t *freeTrack, lit *ast.FuncLit, s ostate) ostate {
	if !flatLit(lit) {
		if litMentions(t.pass, lit, t.v) {
			return stUnknown
		}
		return s
	}
	for _, sb := range lit.Body.List {
		s = freeStep(t, lit, sb, s)
	}
	return s
}

func freeStep(t *freeTrack, lit ast.Node, sb ast.Stmt, s ostate) ostate {
	if inner, ok := inPlaceLit(sb); ok {
		return freeLitState(t, inner, s)
	}
	if stmtLitWrites(t.pass, sb, t.v) {
		return stUnknown
	}
	if hasFreeRelease(t.pass, lit, sb, t.v, t.cache) {
		return stReleased
	}
	if keepsReleased(t.pass, sb, t.v) {
		return s
	}
	if assignsVar(t.pass, t.fn, sb, t.v) {
		return stUnknown
	}
	if escapesVar(t.pass, t.fn, sb, t.v) {
		return stUnknown
	}
	return s
}

func freeLitReleasesHeld(t *freeTrack, lit *ast.FuncLit, s ostate) bool {
	if !flatLit(lit) {
		return false
	}
	cur := s
	for _, sb := range lit.Body.List {
		var done bool
		cur, done = freeHeldNext(t, lit, sb, cur)
		if done {
			return true
		}
	}
	return false
}

func freeHeldNext(t *freeTrack, lit ast.Node, sb ast.Stmt, cur ostate) (ostate, bool) {
	if inner, ok := inPlaceLit(sb); ok {
		if freeLitReleasesHeld(t, inner, cur) {
			return cur, true
		}
		return freeLitState(t, inner, cur), false
	}
	if stmtLitWrites(t.pass, sb, t.v) {
		return stUnknown, false
	}
	if hasFreeRelease(t.pass, lit, sb, t.v, t.cache) {
		if cur == stReleased {
			return cur, true
		}
		return stReleased, false
	}
	if keepsReleased(t.pass, sb, t.v) {
		return cur, false
	}
	if assignsVar(t.pass, t.fn, sb, t.v) {
		return stUnknown, false
	}
	if escapesVar(t.pass, t.fn, sb, t.v) {
		return stUnknown, false
	}
	return cur, false
}

func stmtLitWrites(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		lit, ok := x.(*ast.FuncLit)
		if !ok {
			return true
		}
		if lit.Body == nil {
			return false
		}
		if litWritesVar(pass, lit, v) {
			found = true
		}
		return false
	})
	return found
}

func litWritesVar(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) bool {
	writes := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if writes {
			return false
		}
		if litWriteNode(pass, n, v) {
			writes = true
			return false
		}
		return true
	})
	return writes
}

func litWriteNode(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return lhsAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return incAssigns(pass, x.X, v)
	case *ast.RangeStmt:
		return rangeWritesVar(pass, x, v)
	case *ast.UnaryExpr:
		return addrWritesVar(pass, x, v)
	}
	return false
}

func rangeWritesVar(pass *analysis.Pass, r *ast.RangeStmt, v *types.Var) bool {
	if id, ok := ast.Unparen(r.Key).(*ast.Ident); ok {
		if pass.TypesInfo.Uses[id] == v || pass.TypesInfo.Defs[id] == v {
			return true
		}
	}
	if id, ok := ast.Unparen(r.Value).(*ast.Ident); ok {
		if pass.TypesInfo.Uses[id] == v || pass.TypesInfo.Defs[id] == v {
			return true
		}
	}
	return false
}

func addrWritesVar(pass *analysis.Pass, x *ast.UnaryExpr, v *types.Var) bool {
	if x.Op != token.AND {
		return false
	}
	k, ok := flow.KeyOf(pass.TypesInfo, x.X)
	return ok && k.Root == v
}

func freeArgsMention(q freeQuery, call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	return freeTargets(q, call.Args[0])
}

func freeTargets(q freeQuery, e ast.Expr) bool {
	e = ast.Unparen(e)
	if exactVar(q.pass, e, q.v) {
		return true
	}
	switch x := e.(type) {
	case *ast.SliceExpr:
		if !wholeSlice(q.pass, x, q.v) {
			return false
		}
		return freeTargets(q, x.X)
	case *ast.CallExpr:
		return freeConvTarget(q, e, x)
	case *ast.Ident:
		return freeTempTarget(q, x)
	}
	return false
}

func wholeSlice(pass *analysis.Pass, x *ast.SliceExpr, v *types.Var) bool {
	if x.X == nil || x.Max != nil {
		return false
	}
	if !exactVar(pass, x.X, v) {
		return false
	}
	if x.Low != nil && !isZeroExpr(pass, x.Low) {
		return false
	}
	if x.High == nil {
		return true
	}
	return isLenOf(pass, x.High, v)
}

func isZeroExpr(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[ast.Unparen(e)]
	if !ok {
		return false
	}
	return tv.Value != nil && tv.Value.String() == "0"
}

func isLenOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if !plainCall(pass, call) {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != lenBuiltin {
		return false
	}
	return exactVar(pass, call.Args[0], v)
}

func freeConvTarget(q freeQuery, e ast.Expr, call *ast.CallExpr) bool {
	if len(call.Args) != 1 || !q.pass.TypesInfo.Types[call.Fun].IsType() {
		return false
	}
	if t := q.pass.TypesInfo.Types[e].Type; t == nil || t.String() != unsafePointerType {
		return false
	}
	return freeTargets(q, call.Args[0])
}

func freeTempTarget(q freeQuery, id *ast.Ident) bool {
	tv, ok := q.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok || tv == q.v {
		return false
	}
	def, ok := q.cache.forScope(q.fn)[tv]
	if !ok {
		return false
	}
	if !freeConvOf(q.pass, def, q.v) && !exactVar(q.pass, def, q.v) {
		return false
	}
	return !tempSourceReassigned(q.pass, q.fn, q.release, def, q.v)
}

func tempSourceReassigned(pass *analysis.Pass, scope, release ast.Node, def ast.Expr, v *types.Var) bool {
	if sourceLitWrites(pass, scope, v) {
		return true
	}
	if g := flow.NewGraph(pass, scope); g != nil {
		return sourceReachesRelease(pass, scope, release, g, def, v)
	}
	return sourceBetweenPositions(pass, scope, release, def, v)
}

func writesSource(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	if as, ok := n.(*ast.AssignStmt); ok {
		return lhsAssigns(pass, as.Lhs, v)
	}
	if r, ok := n.(*ast.RangeStmt); ok {
		return rangeWritesVar(pass, r, v)
	}
	return false
}

func sourceLitWrites(pass *analysis.Pass, scope ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(scope, func(n ast.Node) bool {
		if found {
			return false
		}
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit == scope {
			return true
		}
		if litWritesSource(pass, lit, v) {
			found = true
		}
		return !found
	})
	return found
}

func litWritesSource(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) bool {
	writes := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if writes {
			return false
		}
		if writesSource(pass, n, v) {
			writes = true
		}
		return !writes
	})
	return writes
}

type graphScan struct {
	pass       *analysis.Pass
	scope      ast.Node
	release    ast.Node
	g          *flow.Graph
	def        ast.Expr
	v          *types.Var
	reassigned bool
}

func sourceReachesRelease(pass *analysis.Pass, scope, release ast.Node, g *flow.Graph, def ast.Expr, v *types.Var) bool {
	s := &graphScan{pass: pass, scope: scope, release: release, g: g, def: def, v: v}
	ast.Inspect(scope, s.visit)
	return s.reassigned
}

func (s *graphScan) visit(n ast.Node) bool {
	if s.reassigned || freeTempSkip(n, s.scope) {
		return false
	}
	if writesSource(s.pass, n, s.v) && betweenOnGraph(s.g, s.def, n, s.release) {
		s.reassigned = true
	}
	return !s.reassigned
}

func betweenOnGraph(g *flow.Graph, def, w, release ast.Node) bool {
	return g.Reaches(def, w, nil) && g.Reaches(w, release, nil)
}

type loopSpan struct {
	pos token.Pos
	end token.Pos
}

func collectLoopSpans(scope ast.Node) []loopSpan {
	var out []loopSpan
	ast.Inspect(scope, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ForStmt:
			out = append(out, loopSpan{pos: x.Pos(), end: x.End()})
		case *ast.RangeStmt:
			out = append(out, loopSpan{pos: x.Pos(), end: x.End()})
		}
		return true
	})
	return out
}

func spanEncloses(spans []loopSpan, w, release ast.Node) bool {
	for _, s := range spans {
		if s.pos <= w.Pos() && w.Pos() <= s.end && s.pos <= release.Pos() && release.Pos() <= s.end {
			return true
		}
	}
	return false
}

func betweenPositions(def ast.Expr, w, release ast.Node) bool {
	return def.Pos() < w.Pos() && w.Pos() < release.Pos()
}

type posScan struct {
	pass       *analysis.Pass
	scope      ast.Node
	release    ast.Node
	def        ast.Expr
	v          *types.Var
	spans      []loopSpan
	reassigned bool
}

func sourceBetweenPositions(pass *analysis.Pass, scope, release ast.Node, def ast.Expr, v *types.Var) bool {
	s := &posScan{pass: pass, scope: scope, release: release, def: def, v: v, spans: collectLoopSpans(scope)}
	ast.Inspect(scope, s.visit)
	return s.reassigned
}

func (s *posScan) visit(n ast.Node) bool {
	if s.reassigned || freeTempSkip(n, s.scope) {
		return false
	}
	if !writesSource(s.pass, n, s.v) {
		return true
	}
	if betweenPositions(s.def, n, s.release) {
		s.reassigned = true
		return false
	}
	if spanEncloses(s.spans, n, s.release) {
		s.reassigned = true
	}
	return !s.reassigned
}

type freeTempCache struct {
	pass    *analysis.Pass
	byScope map[ast.Node]map[*types.Var]ast.Expr
}

func newFreeTempCache(pass *analysis.Pass) *freeTempCache {
	return &freeTempCache{pass: pass, byScope: map[ast.Node]map[*types.Var]ast.Expr{}}
}

func (c *freeTempCache) forScope(scope ast.Node) map[*types.Var]ast.Expr {
	if m, ok := c.byScope[scope]; ok {
		return m
	}
	m := buildFreeTempIndex(c.pass, scope)
	c.byScope[scope] = m
	return m
}

func buildFreeTempIndex(pass *analysis.Pass, scope ast.Node) map[*types.Var]ast.Expr {
	s := &tempIndexScan{pass: pass, scope: scope, rhsOf: map[*types.Var]ast.Expr{}, bad: map[*types.Var]bool{}}
	ast.Inspect(scope, s.visit)
	return s.rhsOf
}

type tempIndexScan struct {
	pass  *analysis.Pass
	scope ast.Node
	rhsOf map[*types.Var]ast.Expr
	bad   map[*types.Var]bool
}

func (s *tempIndexScan) visit(n ast.Node) bool {
	if freeTempSkip(n, s.scope) {
		return false
	}
	if r, ok := n.(*ast.RangeStmt); ok {
		if r.Tok == token.ASSIGN {
			markFreeTempRange(s.pass, r, s.rhsOf, s.bad)
		}
		return true
	}
	as, ok := n.(*ast.AssignStmt)
	if !ok {
		return true
	}
	if as.Tok == token.DEFINE && len(as.Rhs) == 1 {
		recordFreeTempSingle(s.pass, as, s.rhsOf, s.bad)
	} else {
		markFreeTempBad(s.pass, as, s.rhsOf, s.bad)
	}
	return true
}

func recordFreeTempSingle(pass *analysis.Pass, as *ast.AssignStmt, rhsOf map[*types.Var]ast.Expr, bad map[*types.Var]bool) {
	for _, lhs := range as.Lhs {
		lid, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		tv := boundTempVar(pass, as, lid)
		if tv == nil || bad[tv] {
			continue
		}
		if _, seen := rhsOf[tv]; seen {
			delete(rhsOf, tv)
			bad[tv] = true
			continue
		}
		rhsOf[tv] = as.Rhs[0]
	}
}

func markFreeTempBad(pass *analysis.Pass, as *ast.AssignStmt, rhsOf map[*types.Var]ast.Expr, bad map[*types.Var]bool) {
	for _, lhs := range as.Lhs {
		lid, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		tv := boundTempVar(pass, as, lid)
		if tv == nil {
			continue
		}
		delete(rhsOf, tv)
		bad[tv] = true
	}
}

func markFreeTempRange(pass *analysis.Pass, r *ast.RangeStmt, rhsOf map[*types.Var]ast.Expr, bad map[*types.Var]bool) {
	for _, e := range []ast.Expr{r.Key, r.Value} {
		lid, ok := ast.Unparen(e).(*ast.Ident)
		if !ok || lid == nil {
			continue
		}
		tv, ok := pass.TypesInfo.Uses[lid].(*types.Var)
		if !ok {
			continue
		}
		delete(rhsOf, tv)
		bad[tv] = true
	}
}

func boundTempVar(pass *analysis.Pass, as *ast.AssignStmt, lid *ast.Ident) *types.Var {
	if as.Tok == token.DEFINE {
		if w, ok := pass.TypesInfo.Defs[lid].(*types.Var); ok {
			return w
		}
	}
	if w, ok := pass.TypesInfo.Uses[lid].(*types.Var); ok {
		return w
	}
	return nil
}

func freeTempSkip(n, scope ast.Node) bool {
	lit, ok := n.(*ast.FuncLit)
	return ok && lit != scope
}

func freeConvOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !pass.TypesInfo.Types[call.Fun].IsType() {
		return false
	}
	if t := pass.TypesInfo.Types[e].Type; t == nil || t.String() != unsafePointerType {
		return false
	}
	return exactVar(pass, call.Args[0], v)
}
