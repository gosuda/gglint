package resstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the four use-after-release analyzers.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		useaftercloseAnalyzer,
		useafterputAnalyzer,
		useafterfreeAnalyzer,
		viewescapeAnalyzer,
	}
}

var useaftercloseAnalyzer = &analysis.Analyzer{
	Name:     "useafterclose",
	Doc:      "reports Read, Write, Seek, and similar methods on os, net, or net/http values after Close on every path; a use inside a function literal is not checked",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUseafterclose,
}

var useafterputAnalyzer = &analysis.Analyzer{
	Name:     "useafterput",
	Doc:      "reports uses of pointer, slice, or map values after they are passed to Put on every path; a use inside a function literal is not checked",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUseafterput,
}

var useafterfreeAnalyzer = &analysis.Analyzer{
	Name:     "useafterfree",
	Doc:      "reports uses of values freed on every path to the use; a value held in a uintptr variable is not tracked; a literal called in place counts as its statements when its body has no control flow, otherwise the state is unknown; a free and a later use inside the same literal are not reported for a value declared outside it; a free through an alias is not followed when a function literal assigns the source of the alias",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUseafterfree,
}

var viewescapeAnalyzer = &analysis.Analyzer{
	Name:     "viewescape",
	Doc:      "reports Scanner.Bytes, Buffer.Bytes, Reader.Peek, ReadSlice views kept past a call that may overwrite them; a receiver declared in the loop body is checked against later iterations only when it names a variable, field, or constant-index element from outside the loop that some path round the loop does not reassign; a view variable that a function literal assigns is not tracked; a call counts only when the receiver is not assigned between the view and the call",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runViewescape,
}

type ostate uint8

const (
	stLive ostate = iota
	stReleased
	stUnknown
)

func joinState(a, b ostate) ostate {
	if a == b {
		return a
	}
	return stUnknown
}

func runUseafterclose(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		closeFunc(pass, cur.Node())
	}
	return nil, nil
}

func runUseafterput(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		putFunc(pass, cur.Node())
	}
	return nil, nil
}

func runUseafterfree(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		freeFunc(pass, cur.Node())
	}
	return nil, nil
}

func closeFunc(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	cands := closeCandidates(pass, fn)
	if len(cands) == 0 {
		return
	}
	for _, v := range localsOf(pass, fn) {
		if !hasCloseResult(v.Type()) {
			continue
		}
		if !cands[v] {
			continue
		}
		closeVar(pass, fn, g, v)
	}
}

func closeCandidates(pass *analysis.Pass, fn ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		if v, ok := closeCandidateOf(pass, n); ok {
			out[v] = true
		}
		return true
	})
	return out
}

func closeCandidateOf(pass *analysis.Pass, n ast.Node) (*types.Var, bool) {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return nil, false
	}
	recv, ok := closeRecv(pass, call)
	if !ok {
		return nil, false
	}
	k, ok := flow.KeyOf(pass.TypesInfo, recv)
	if !ok || k.Path != "" {
		return nil, false
	}
	v, ok := k.Root.(*types.Var)
	return v, ok
}

func putFunc(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	cands := putCandidates(pass, fn)
	if len(cands) == 0 {
		return
	}
	for _, v := range localsOf(pass, fn) {
		if !isPoolValue(v.Type()) {
			continue
		}
		if !cands[v] {
			continue
		}
		putVar(pass, fn, g, v)
	}
}

func freeFunc(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	cands := freeCandidates(pass, fn)
	if len(cands) == 0 {
		return
	}
	cache := newFreeTempCache(pass)
	for _, v := range localsOf(pass, fn) {
		if !cands[v] {
			continue
		}
		freeVar(pass, fn, g, v, cache)
	}
}

func putCandidates(pass *analysis.Pass, fn ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		if v, ok := putCandidateOf(pass, n); ok {
			out[v] = true
		}
		return true
	})
	return out
}

func putCandidateOf(pass *analysis.Pass, n ast.Node) (*types.Var, bool) {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != putMethod {
		return nil, false
	}
	if !isPoolType(pass.TypesInfo.Types[sel.X].Type) {
		return nil, false
	}
	k, ok := flow.KeyOf(pass.TypesInfo, call.Args[0])
	if !ok || k.Path != "" {
		return nil, false
	}
	v, ok := k.Root.(*types.Var)
	return v, ok
}

func freeCandidates(pass *analysis.Pass, fn ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	defMap := buildDefMap(pass, fn)
	ast.Inspect(fn, func(n ast.Node) bool {
		es, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		call, ok := ast.Unparen(es.X).(*ast.CallExpr)
		if !ok {
			return true
		}
		if len(call.Args) == 0 {
			if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok && lit.Body != nil {
				collectLitFreeVars(pass, defMap, lit, out)
			}
			return true
		}
		if !freeCallName(call) {
			return true
		}
		collectFreeArgVars(pass, defMap, call.Args[0], out)
		return true
	})
	return out
}

func collectLitFreeVars(pass *analysis.Pass, defMap map[*types.Var][]ast.Expr, lit *ast.FuncLit, out map[*types.Var]bool) {
	for _, s := range lit.Body.List {
		es, ok := s.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := ast.Unparen(es.X).(*ast.CallExpr)
		if !ok {
			continue
		}
		if len(call.Args) == 0 {
			if inner, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok && inner.Body != nil {
				collectLitFreeVars(pass, defMap, inner, out)
			}
			continue
		}
		if !freeCallName(call) {
			continue
		}
		collectFreeArgVars(pass, defMap, call.Args[0], out)
	}
}

func freeCallName(call *ast.CallExpr) bool {
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		return sel.Sel.Name == "Munmap" || sel.Sel.Name == "free"
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		return id.Name == cFreeFunc
	}
	return false
}

func collectFreeArgVars(pass *analysis.Pass, defMap map[*types.Var][]ast.Expr, arg ast.Expr, out map[*types.Var]bool) {
	for _, id := range freeBaseIdents(pass, arg) {
		v, ok := pass.TypesInfo.Uses[id].(*types.Var)
		if !ok || out[v] || !isFreeableVar(v) {
			continue
		}
		out[v] = true
		collectTempSources(pass, defMap, v, out)
	}
}

func freeBaseIdents(pass *analysis.Pass, e ast.Expr) []*ast.Ident {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.Ident:
		return []*ast.Ident{x}
	case *ast.SliceExpr:
		if x.X == nil {
			return nil
		}
		return freeBaseIdents(pass, x.X)
	case *ast.CallExpr:
		if len(x.Args) != 1 || !pass.TypesInfo.Types[x.Fun].IsType() {
			return nil
		}
		return freeBaseIdents(pass, x.Args[0])
	default:
		return nil
	}
}

func isFreeableVar(v *types.Var) bool {
	t := v.Type()
	if t == nil {
		return false
	}
	u := types.Unalias(t).Underlying()
	if u == nil {
		return false
	}
	if u.String() == unsafePointerType {
		return true
	}
	switch u.(type) {
	case *types.Slice, *types.Pointer:
		return true
	default:
		return false
	}
}

func buildDefMap(pass *analysis.Pass, fn ast.Node) map[*types.Var][]ast.Expr {
	out := map[*types.Var][]ast.Expr{}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			defMapAssign(pass, x, out)
		case *ast.ValueSpec:
			defMapValueSpec(pass, x, out)
		}
		return true
	})
	return out
}

func defMapAssign(pass *analysis.Pass, x *ast.AssignStmt, out map[*types.Var][]ast.Expr) {
	for _, l := range x.Lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok {
			out[v] = append(out[v], x.Rhs...)
		} else if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
			out[v] = append(out[v], x.Rhs...)
		}
	}
}

func defMapValueSpec(pass *analysis.Pass, x *ast.ValueSpec, out map[*types.Var][]ast.Expr) {
	for _, id := range x.Names {
		if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok {
			out[v] = append(out[v], x.Values...)
		}
	}
}

func collectTempSources(pass *analysis.Pass, defMap map[*types.Var][]ast.Expr, tv *types.Var, out map[*types.Var]bool) {
	collectBaseIdents(pass, defMap[tv], out)
}

func collectBaseIdents(pass *analysis.Pass, exprs []ast.Expr, out map[*types.Var]bool) {
	for _, e := range exprs {
		for _, id := range freeBaseIdents(pass, e) {
			if w, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && isFreeableVar(w) {
				out[w] = true
			} else if w, ok := pass.TypesInfo.Defs[id].(*types.Var); ok && isFreeableVar(w) {
				out[w] = true
			}
		}
	}
}

func closeVar(pass *analysis.Pass, fn ast.Node, g *flow.Graph, v *types.Var) {
	flow.Forward(g, stLive,
		func(n ast.Node, s ostate) ostate { return closeTransfer(pass, fn, n, v, s) },
		joinState,
		func(n ast.Node, s ostate) { closeVisit(pass, n, v, s) })
}

func putVar(pass *analysis.Pass, fn ast.Node, g *flow.Graph, v *types.Var) {
	flow.Forward(g, stLive,
		func(n ast.Node, s ostate) ostate { return putTransfer(pass, fn, n, v, s) },
		joinState,
		func(n ast.Node, s ostate) { putVisit(pass, fn, n, v, s) })
}

func freeVar(pass *analysis.Pass, fn ast.Node, g *flow.Graph, v *types.Var, cache *freeTempCache) {
	flow.Forward(g, stLive,
		func(n ast.Node, s ostate) ostate { return freeTransfer(pass, fn, n, v, s, cache) },
		joinState,
		func(n ast.Node, s ostate) { freeVisit(pass, fn, n, v, s, cache) })
}

func closeTransfer(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) ostate {
	if stmtLitWrites(pass, n, v) {
		return stUnknown
	}
	if hasCloseRelease(pass, n, v) {
		return stReleased
	}
	if assignsVar(pass, fn, n, v) {
		return stUnknown
	}
	if escapesVar(pass, fn, n, v) {
		return stUnknown
	}
	return s
}

func putTransfer(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) ostate {
	if stmtLitWrites(pass, n, v) {
		return stUnknown
	}
	if hasPutRelease(pass, n, v) {
		return stReleased
	}
	if keepsReleased(pass, n, v) {
		return s
	}
	if assignsVar(pass, fn, n, v) {
		return stUnknown
	}
	if escapesVar(pass, fn, n, v) {
		return stUnknown
	}
	return s
}

func freeTransfer(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate, cache *freeTempCache) ostate {
	if lit, ok := inPlaceLit(n); ok {
		return freeLitState(&freeTrack{pass: pass, fn: fn, v: v, cache: cache}, lit, s)
	}
	if stmtLitWrites(pass, n, v) {
		return stUnknown
	}
	if hasFreeRelease(pass, fn, n, v, cache) {
		return stReleased
	}
	if keepsReleased(pass, n, v) {
		return s
	}
	if assignsVar(pass, fn, n, v) {
		return stUnknown
	}
	if escapesVar(pass, fn, n, v) {
		return stUnknown
	}
	return s
}

func keepsReleased(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	if !lhsAssigns(pass, as.Lhs, v) {
		return false
	}
	rhs := ast.Unparen(as.Rhs[0])
	if id, ok := rhs.(*ast.Ident); ok {
		return pass.TypesInfo.Uses[id] == v
	}
	return sliceChainOf(pass, rhs, v)
}

func sliceChainOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		return pass.TypesInfo.Uses[id] == v || pass.TypesInfo.Defs[id] == v
	}
	se, ok := e.(*ast.SliceExpr)
	if !ok || se.X == nil {
		return false
	}
	return sliceChainOf(pass, se.X, v)
}

func closeVisit(pass *analysis.Pass, n ast.Node, v *types.Var, s ostate) {
	if s != stReleased {
		return
	}
	for _, c := range methodCallsOn(pass, n, v) {
		if !closeFailsOn(pass, c) {
			continue
		}
		pass.Reportf(c.Pos(), "%s is closed on every path to this call, so the call fails: move Close after the last use", v.Name())
	}
}

func putVisit(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) {
	if s != stReleased {
		return
	}
	if nilAssign(pass, n, v) {
		return
	}
	if selfAssign(pass, n, v) {
		return
	}
	if hasPutRelease(pass, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s was returned to the pool, and another goroutine may own it now: call Put after the last use", v.Name())
		return
	}
	if takesAddr(pass, n, v) {
		return
	}
	if redefinesVar(pass, fn, n, v) {
		return
	}
	if mentionsVar(pass, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s was returned to the pool, and another goroutine may own it now: call Put after the last use", v.Name())
		return
	}
}

func freeVisit(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate, cache *freeTempCache) {
	if s != stReleased {
		return
	}
	if lit, ok := inPlaceLit(n); ok {
		visitInPlaceFree(&freeTrack{pass: pass, fn: fn, v: v, cache: cache}, n, lit, s)
		return
	}
	if nilAssign(pass, n, v) {
		return
	}
	if selfAssign(pass, n, v) {
		return
	}
	reportFreedUse(pass, fn, n, v, cache)
}

func visitInPlaceFree(t *freeTrack, n ast.Node, lit *ast.FuncLit, s ostate) {
	if freeLitReleasesHeld(t, lit, s) {
		t.pass.Reportf(mentionPos(t.pass, n, t.v), "%s is used after it was freed: set it to nil after the free and do not use it again", t.v.Name())
	}
	reportUsesInLit(t, lit, s)
}

func reportUsesInLit(t *freeTrack, lit *ast.FuncLit, s ostate) {
	if !flatLit(lit) {
		return
	}
	cur := s
	for _, sb := range lit.Body.List {
		if inner, ok := inPlaceLit(sb); ok {
			reportUsesInLit(t, inner, cur)
			cur = freeLitState(t, inner, cur)
			continue
		}
		if cur == stReleased && freedUseAt(t, lit, sb) {
			t.pass.Reportf(mentionPos(t.pass, sb, t.v), "%s is used after it was freed: set it to nil after the free and do not use it again", t.v.Name())
		}
		cur = freeStep(t, lit, sb, cur)
	}
}

func freedUseAt(t *freeTrack, scope ast.Node, sb ast.Stmt) bool {
	if hasFreeRelease(t.pass, scope, sb, t.v, t.cache) {
		return false
	}
	if nilAssign(t.pass, sb, t.v) {
		return false
	}
	if selfAssign(t.pass, sb, t.v) {
		return false
	}
	if takesAddr(t.pass, sb, t.v) {
		return false
	}
	if redefinesVar(t.pass, t.fn, sb, t.v) {
		return false
	}
	return mentionsVar(t.pass, sb, t.v)
}

func reportFreedUse(pass *analysis.Pass, fn, n ast.Node, v *types.Var, cache *freeTempCache) {
	if hasFreeRelease(pass, fn, n, v, cache) {
		pass.Reportf(mentionPos(pass, n, v), "%s is used after it was freed: set it to nil after the free and do not use it again", v.Name())
		return
	}
	if takesAddr(pass, n, v) {
		return
	}
	if redefinesVar(pass, fn, n, v) {
		return
	}
	if mentionsVar(pass, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s is used after it was freed: set it to nil after the free and do not use it again", v.Name())
		return
	}
}

func nilAssign(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	id, ok := ast.Unparen(as.Lhs[0]).(*ast.Ident)
	if !ok || pass.TypesInfo.Uses[id] != v {
		return false
	}
	return pass.TypesInfo.Types[as.Rhs[0]].IsNil()
}

func selfAssign(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	if se, ok := ast.Unparen(as.Rhs[0]).(*ast.SliceExpr); ok {
		return selfSliceAssign(pass, as, se, v)
	}
	return selfIdentAssign(pass, as, v)
}

func selfSliceAssign(pass *analysis.Pass, as *ast.AssignStmt, se *ast.SliceExpr, v *types.Var) bool {
	if !exactVar(pass, se.X, v) {
		return false
	}
	if isBlankIdent(as.Lhs[0]) {
		return true
	}
	lid, lok := ast.Unparen(as.Lhs[0]).(*ast.Ident)
	return lok && (pass.TypesInfo.Uses[lid] == v || pass.TypesInfo.Defs[lid] == v)
}

func selfIdentAssign(pass *analysis.Pass, as *ast.AssignStmt, v *types.Var) bool {
	if isBlankIdent(as.Lhs[0]) {
		rid, rok := ast.Unparen(as.Rhs[0]).(*ast.Ident)
		return rok && pass.TypesInfo.Uses[rid] == v
	}
	lid, lok := ast.Unparen(as.Lhs[0]).(*ast.Ident)
	if !lok || pass.TypesInfo.Uses[lid] != v {
		return false
	}
	rid, rok := ast.Unparen(as.Rhs[0]).(*ast.Ident)
	return rok && pass.TypesInfo.Uses[rid] == v
}

func isBlankIdent(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "_"
}

func localsOf(pass *analysis.Pass, fn ast.Node) []*types.Var {
	seen := newLocalSeen(pass, fn)
	ast.Inspect(fn, func(n ast.Node) bool {
		if skipNestedLit(n, fn) {
			return false
		}
		if cc, ok := n.(*ast.CaseClause); ok {
			seen.addImplicit(cc)
			return true
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		seen.addDef(id)
		return true
	})
	return append([]*types.Var(nil), seen.order...)
}

type localSeen struct {
	pass   *analysis.Pass
	params map[*types.Var]bool
	seen   map[*types.Var]bool
	order  []*types.Var
}

func newLocalSeen(pass *analysis.Pass, fn ast.Node) *localSeen {
	return &localSeen{pass: pass, params: paramVarSet(pass, fn), seen: map[*types.Var]bool{}}
}

func (s *localSeen) addImplicit(cc *ast.CaseClause) {
	v, ok := s.pass.TypesInfo.Implicits[cc].(*types.Var)
	if !ok || s.seen[v] || s.params[v] || v.IsField() {
		return
	}
	s.seen[v] = true
	s.order = append(s.order, v)
}

func (s *localSeen) addDef(id *ast.Ident) {
	v, ok := s.pass.TypesInfo.Defs[id].(*types.Var)
	if !ok || s.seen[v] || s.params[v] || v.IsField() {
		return
	}
	s.seen[v] = true
	s.order = append(s.order, v)
}

func skipNestedLit(n, fn ast.Node) bool {
	if n == fn {
		return false
	}
	_, ok := n.(*ast.FuncLit)
	return ok
}

func paramVarSet(pass *analysis.Pass, fn ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ft, recv := funcTypeOf(fn)
	if ft == nil {
		return out
	}
	addFieldVars(pass, ft.Params, out)
	addFieldVars(pass, ft.Results, out)
	addFieldVars(pass, recv, out)
	return out
}

func funcTypeOf(fn ast.Node) (*ast.FuncType, *ast.FieldList) {
	switch f := fn.(type) {
	case *ast.FuncDecl:
		if f.Type == nil {
			return nil, nil
		}
		return f.Type, f.Recv
	case *ast.FuncLit:
		if f.Type == nil {
			return nil, nil
		}
		return f.Type, nil
	default:
		return nil, nil
	}
}

func addFieldVars(pass *analysis.Pass, fields *ast.FieldList, out map[*types.Var]bool) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, id := range field.Names {
			if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok {
				out[v] = true
			}
		}
	}
}

func hasCloseResult(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Close")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type())
}

func isPoolType(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Pool" && obj.Pkg() != nil && obj.Pkg().Path() == "sync"
}

func isPoolValue(t types.Type) bool {
	switch types.Unalias(t).Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map:
		return true
	}
	return false
}
