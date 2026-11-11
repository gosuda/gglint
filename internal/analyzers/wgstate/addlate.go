package wgstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

var addlateAnalyzer = &analysis.Analyzer{
	Name:     "addlate",
	Doc:      "report a WaitGroup Add that is a plain statement after a go statement whose function literal calls Done, in the same statement list; an Add earlier in the function silences it, also an Add inside a function literal that is called before the go statement directly or through a local variable that can hold it at that call; an Add in a go statement never counts as earlier, and an Add in a defer statement in the function itself does not, while a defer inside a called literal does; a literal that receives or sends, ranges over a channel, or locks or waits is not reported, because that wait can order the Add; the check cannot know the capacity of every channel, so it stays silent for a buffered channel too; a call to another package or through a function value is assumed not to wait; a call passed the WaitGroup counts unless it is a same-package function whose body neither Adds on the receiving parameter directly, through an alias, or inside a literal, nor hands it on",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runAddlate,
}

type lateGroup struct {
	list  []ast.Stmt
	goIdx int
	seen  map[token.Pos]bool
}

func runAddlate(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		fn := cur.Node()
		checkAddlate(pass, fn, enclosingFuncBodies(cur, fn))
	}
	return nil, nil
}

func checkAddlate(pass *analysis.Pass, fn ast.Node, enclosing []*ast.BlockStmt) {
	first := firstAddPositions(pass, fn, enclosing)
	eachList(fn, func(list []ast.Stmt) {
		seen := map[token.Pos]bool{}
		for i, s := range list {
			g, ok := unwrap(s).(*ast.GoStmt)
			if !ok {
				continue
			}
			checkGoAddlate(pass, fn, g, lateGroup{list: list, goIdx: i, seen: seen}, first, enclosing)
		}
	})
}

func firstAddPositions(pass *analysis.Pass, fn ast.Node, enclosing []*ast.BlockStmt) map[flow.Key]token.Pos {
	out := map[flow.Key]token.Pos{}
	body := funcBody(fn)
	if body == nil {
		return out
	}
	collectDirectAdds(pass, body, out)
	mergeInPlaceAdds(pass, body, out)
	mergeVarLitAdds(pass, fn, body, out)
	if containsDirectGo(body) {
		mergeEnclosingAdds(enclosing, fn.Pos(), pass, out)
	}
	return out
}

func collectDirectAdds(pass *analysis.Pass, body *ast.BlockStmt, out map[flow.Key]token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ak, _, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok {
			return true
		}
		if p, dup := out[ak]; !dup || call.Pos() < p {
			out[ak] = call.Pos()
		}
		return true
	})
}

func mergeEnclosingAdds(enclosing []*ast.BlockStmt, before token.Pos, pass *analysis.Pass, out map[flow.Key]token.Pos) {
	for _, body := range enclosing {
		collectEnclosingAdds(pass, body, before, out)
	}
}

func enclosingFuncBodies(cur inspector.Cursor, fn ast.Node) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	for enc := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		if enc.Node() == fn {
			continue
		}
		if body := funcBody(enc.Node()); body != nil {
			out = append(out, body)
		}
	}
	return out
}

func containsDirectGo(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

func collectEnclosingAdds(pass *analysis.Pass, body *ast.BlockStmt, before token.Pos, out map[flow.Key]token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= before {
			return true
		}
		ak, _, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok {
			return true
		}
		if p, dup := out[ak]; !dup || call.Pos() < p {
			out[ak] = call.Pos()
		}
		return true
	})
}

func litAddKeys(pass *analysis.Pass, lit *ast.FuncLit) []flow.Key {
	seen := map[flow.Key]bool{}
	var out []flow.Key
	if lit.Body == nil {
		return out
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, _, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok || seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return out
}

func mergeInPlaceAdds(pass *analysis.Pass, body *ast.BlockStmt, out map[flow.Key]token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
		if !ok {
			return true
		}
		for _, k := range litAddKeys(pass, lit) {
			if p, dup := out[k]; !dup || call.Pos() < p {
				out[k] = call.Pos()
			}
		}
		return true
	})
}

func mergeVarLitAdds(pass *analysis.Pass, fn ast.Node, body *ast.BlockStmt, out map[flow.Key]token.Pos) {
	set := newVarLitSet()
	collectVarLits(pass.TypesInfo, body, set)
	applyVarLits(pass, fn, set, out)
}

type varAssign struct {
	lit   *ast.FuncLit
	pos   token.Pos
	inLit bool
	loops []ast.Node
}

type varCall struct {
	pos   token.Pos
	loops []ast.Node
}

type varLitSet struct {
	assigns map[*types.Var][]varAssign
	calls   map[*types.Var][]varCall
}

func newVarLitSet() *varLitSet {
	return &varLitSet{assigns: map[*types.Var][]varAssign{}, calls: map[*types.Var][]varCall{}}
}

type scopeView struct {
	loops  []ast.Node
	lits   []*ast.FuncLit
	blocks []ast.Node
}

func containsNode(outer ast.Node, pos, end token.Pos) bool {
	return outer.Pos() < pos && end <= outer.End()
}

func enclosingLoops(pos, end token.Pos, loops []ast.Node) []ast.Node {
	var out []ast.Node
	for _, l := range loops {
		if containsNode(l, pos, end) {
			out = append(out, l)
		}
	}
	return out
}

func insideLits(pos, end token.Pos, lits []*ast.FuncLit) bool {
	for _, f := range lits {
		if containsNode(f, pos, end) {
			return true
		}
	}
	return false
}

func insideSkips(pos, end token.Pos, view scopeView) bool {
	if insideLits(pos, end, view.lits) {
		return true
	}
	for _, b := range view.blocks {
		if containsNode(b, pos, end) {
			return true
		}
	}
	return false
}

func sharesLoop(a, b []ast.Node) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func canInvoke(a varAssign, c varCall) bool {
	if a.pos < c.pos {
		return true
	}
	if a.inLit {
		return true
	}
	return sharesLoop(a.loops, c.loops)
}

func scopeViewOf(body *ast.BlockStmt) scopeView {
	var view scopeView
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ForStmt:
			view.loops = append(view.loops, n)
		case *ast.RangeStmt:
			view.loops = append(view.loops, n)
		case *ast.FuncLit:
			view.lits = append(view.lits, n)
		case *ast.GoStmt:
			view.blocks = append(view.blocks, n)
		case *ast.DeferStmt:
			view.blocks = append(view.blocks, n)
		}
		return true
	})
	return view
}

func recordAssignLits(info *types.Info, body *ast.BlockStmt, set *varLitSet, view scopeView) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			noteAssignLit(info, n, set, view)
		case *ast.ValueSpec:
			noteValueLit(info, n, set, view)
		}
		return true
	})
}

func recordVarCalls(info *types.Info, body *ast.BlockStmt, set *varLitSet, view scopeView) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		case *ast.CallExpr:
			noteVarCall(info, n, set, view)
		}
		return true
	})
}

func collectVarLits(info *types.Info, body *ast.BlockStmt, set *varLitSet) {
	view := scopeViewOf(body)
	recordAssignLits(info, body, set, view)
	recordVarCalls(info, body, set, view)
}

func applyVarLits(pass *analysis.Pass, fn ast.Node, set *varLitSet, out map[flow.Key]token.Pos) {
	for v := range set.assigns {
		applyOneVarLit(pass, fn, v, set, out)
	}
}

func recordEarliest(out map[flow.Key]token.Pos, keys []flow.Key, pos token.Pos) {
	for _, k := range keys {
		if p, dup := out[k]; !dup || pos < p {
			out[k] = pos
		}
	}
}

func applyOneVarLit(pass *analysis.Pass, fn ast.Node, v *types.Var, set *varLitSet, out map[flow.Key]token.Pos) {
	if !flow.IsLocal(pass.TypesInfo, fn, v) {
		return
	}
	assigns, ok := set.assigns[v]
	if !ok {
		return
	}
	calls, ok := set.calls[v]
	if !ok {
		return
	}
	for _, a := range assigns {
		keys := litAddKeys(pass, a.lit)
		for _, c := range calls {
			if !canInvoke(a, c) {
				continue
			}
			recordEarliest(out, keys, c.pos)
		}
	}
}

func noteAssignLit(info *types.Info, s *ast.AssignStmt, set *varLitSet, view scopeView) {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}
	id, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	lit, ok := ast.Unparen(s.Rhs[0]).(*ast.FuncLit)
	if !ok {
		return
	}
	set.assigns[v] = append(set.assigns[v], varAssign{
		lit:   lit,
		pos:   s.Pos(),
		inLit: insideLits(s.Pos(), s.End(), view.lits),
		loops: enclosingLoops(s.Pos(), s.End(), view.loops),
	})
}

func noteValueLit(info *types.Info, s *ast.ValueSpec, set *varLitSet, view scopeView) {
	if len(s.Names) != 1 || len(s.Values) != 1 {
		return
	}
	v, ok := info.ObjectOf(s.Names[0]).(*types.Var)
	if !ok {
		return
	}
	lit, ok := ast.Unparen(s.Values[0]).(*ast.FuncLit)
	if !ok {
		return
	}
	set.assigns[v] = append(set.assigns[v], varAssign{
		lit:   lit,
		pos:   s.Pos(),
		inLit: insideLits(s.Pos(), s.End(), view.lits),
		loops: enclosingLoops(s.Pos(), s.End(), view.loops),
	})
}

func noteVarCall(info *types.Info, call *ast.CallExpr, set *varLitSet, view scopeView) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	if insideSkips(call.Pos(), call.End(), view) {
		return
	}
	set.calls[v] = append(set.calls[v], varCall{
		pos:   call.Pos(),
		loops: enclosingLoops(call.Pos(), call.End(), view.loops),
	})
}

func checkGoAddlate(pass *analysis.Pass, fn ast.Node, g *ast.GoStmt, group lateGroup, first map[flow.Key]token.Pos, enclosing []*ast.BlockStmt) {
	lit, ok := ast.Unparen(g.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return
	}
	plain, deferred := donePositions(pass, lit)
	if len(plain) == 0 && len(deferred) == 0 {
		return
	}
	if waitsForParent(pass, lit) {
		return
	}
	for _, k := range sortedDoneKeys(plain, deferred) {
		if hasAddBefore(first, g, k) {
			continue
		}
		if possibleAddBefore(pass, fn, enclosing, g, k) {
			continue
		}
		reportLateAdds(pass, group, k)
	}
}

func possibleAddBefore(pass *analysis.Pass, fn ast.Node, enclosing []*ast.BlockStmt, g *ast.GoStmt, k flow.Key) bool {
	if possibleAddInBody(pass, funcBody(fn), g.Pos(), k) {
		return true
	}
	for _, body := range enclosing {
		if possibleAddInBody(pass, body, g.Pos(), k) {
			return true
		}
	}
	return false
}

func possibleAddInBody(pass *analysis.Pass, body *ast.BlockStmt, before token.Pos, k flow.Key) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= before {
			return true
		}
		if possibleAddCall(pass, call, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func possibleAddCall(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	if passesWG(pass, call, k) {
		return passedWGCounts(pass, call, k)
	}
	return calleeAddsSame(pass, call, k)
}

func passedWGCounts(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return true
	}
	decl := helperDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return true
	}
	return calleeUsesParamForAdd(pass, decl, call, k)
}

func calleeUsesParamForAdd(pass *analysis.Pass, decl *ast.FuncDecl, call *ast.CallExpr, k flow.Key) bool {
	params := paramVars(pass, decl)
	for i, pv := range params {
		if i >= len(call.Args) {
			continue
		}
		rk, ok := flow.KeyOf(pass.TypesInfo, call.Args[i])
		if !ok || rk != k {
			continue
		}
		if paramAddsOrHandsOff(pass, decl, pv) {
			return true
		}
	}
	return false
}

func paramAddsOrHandsOff(pass *analysis.Pass, decl *ast.FuncDecl, pv *types.Var) bool {
	found := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if addsOnParam(pass, decl, call, pv) || passesParam(pass, call, pv) {
			found = true
			return false
		}
		return true
	})
	return found
}

func addsOnParam(pass *analysis.Pass, decl *ast.FuncDecl, call *ast.CallExpr, pv *types.Var) bool {
	ak, _, ok := wgKey(pass.TypesInfo, call, "Add")
	if !ok {
		return false
	}
	if ak.Root == pv {
		return true
	}
	v, ok := ak.Root.(*types.Var)
	return ok && aliasesParam(pass, decl, v, pv, call.Pos()) && isWGPointer(v.Type()) && isWGPointer(pv.Type())
}

func isWGPointer(typ types.Type) bool {
	ptr, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "WaitGroup" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "sync"
}

func aliasesParam(pass *analysis.Pass, decl *ast.FuncDecl, v, pv *types.Var, usePos token.Pos) bool {
	seen := map[*types.Var]bool{}
	cur := v
	before := usePos
	for cur != nil && !seen[cur] {
		seen[cur] = true
		r := latestInitKeyBefore(pass, decl, cur, before)
		if !r.found {
			return false
		}
		if r.key.Root == pv {
			return true
		}
		next, ok := r.key.Root.(*types.Var)
		if !ok || !flow.IsLocal(pass.TypesInfo, decl, next) {
			return false
		}
		cur, before = next, r.pos
	}
	return false
}

type initResult struct {
	key   flow.Key
	pos   token.Pos
	found bool
}

type initScanner struct {
	pass   *analysis.Pass
	target *types.Var
	before token.Pos
	best   initResult
}

func (s *initScanner) step(n ast.Node) bool {
	if _, ok := n.(*ast.GoStmt); ok {
		return false
	}
	if stmt, ok := n.(*ast.AssignStmt); ok {
		s.take(assignInitBefore(s.pass, stmt, s.target, s.before))
		return true
	}
	if spec, ok := n.(*ast.ValueSpec); ok {
		s.take(valueInitBefore(s.pass, spec, s.target, s.before))
	}
	return true
}

func (s *initScanner) take(r initResult) {
	if r.found && (!s.best.found || r.pos > s.best.pos) {
		s.best = r
	}
}

func latestInitKeyBefore(pass *analysis.Pass, decl *ast.FuncDecl, v *types.Var, before token.Pos) initResult {
	scanner := &initScanner{pass: pass, target: v, before: before}
	ast.Inspect(decl.Body, scanner.step)
	return scanner.best
}

func assignInitBefore(pass *analysis.Pass, s *ast.AssignStmt, v *types.Var, before token.Pos) initResult {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 || s.Pos() >= before {
		return initResult{}
	}
	id, ok := s.Lhs[0].(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != v {
		return initResult{}
	}
	k, ok := flow.KeyOf(pass.TypesInfo, s.Rhs[0])
	return initResult{key: k, pos: s.Pos(), found: ok}
}

func valueInitBefore(pass *analysis.Pass, s *ast.ValueSpec, v *types.Var, before token.Pos) initResult {
	if len(s.Names) != 1 || len(s.Values) != 1 || s.Pos() >= before {
		return initResult{}
	}
	if pass.TypesInfo.ObjectOf(s.Names[0]) != v {
		return initResult{}
	}
	k, ok := flow.KeyOf(pass.TypesInfo, s.Values[0])
	return initResult{key: k, pos: s.Pos(), found: ok}
}

func passesParam(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var) bool {
	for _, arg := range call.Args {
		if ak, ok := flow.KeyOf(pass.TypesInfo, arg); ok && ak.Root == pv {
			return true
		}
	}
	return false
}

func passesWG(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	for _, arg := range call.Args {
		ak, ok := flow.KeyOf(pass.TypesInfo, arg)
		if !ok {
			continue
		}
		if ak == k {
			return true
		}
	}
	return false
}

func calleeAddsSame(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return false
	}
	decl := helperDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	return bodyAddsSameForCall(pass, decl, call, k)
}

func bodyAddsSameForCall(pass *analysis.Pass, decl *ast.FuncDecl, call *ast.CallExpr, k flow.Key) bool {
	adds := collectBodyAdds(pass, decl.Body)
	recvVar := receiverVar(pass, decl)
	params := paramVars(pass, decl)
	recvKey := callReceiverKey(pass, call)
	for _, ak := range adds {
		if ak == k && isPackageVar(ak.Root, pass) {
			return true
		}
		if matchesReceiver(recvVar, ak, recvKey, k) {
			return true
		}
		if sameParamAdd(call, params, ak, k, pass) {
			return true
		}
	}
	return false
}

func matchesReceiver(recvVar *types.Var, ak, recvKey, k flow.Key) bool {
	return recvVar != nil && ak.Root == recvVar && recvKey.Root == k.Root && joinPath(recvKey.Path, ak.Path) == k.Path
}

func sameParamAdd(call *ast.CallExpr, params []*types.Var, ak, k flow.Key, pass *analysis.Pass) bool {
	for i, pv := range params {
		if ak.Root != pv || i >= len(call.Args) {
			continue
		}
		rk, ok := flow.KeyOf(pass.TypesInfo, call.Args[i])
		if !ok || rk.Root != k.Root {
			continue
		}
		if joinPath(rk.Path, ak.Path) == k.Path {
			return true
		}
	}
	return false
}

func collectBodyAdds(pass *analysis.Pass, body *ast.BlockStmt) []flow.Key {
	var out []flow.Key
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ak, _, ok := wgKey(pass.TypesInfo, call, "Add"); ok {
			out = append(out, ak)
		}
		return true
	})
	return out
}

func waitsForParent(pass *analysis.Pass, lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		if nodeWaits(pass, n) {
			found = true
			return false
		}
		return true
	})
	return found
}

func nodeWaits(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	case *ast.SendStmt:
		return true
	case *ast.RangeStmt:
		return isChanRange(pass, n)
	case *ast.ForStmt:
		return spinsOnLoad(pass, n)
	case *ast.CallExpr:
		return isSyncWait(pass, n) || helperWaits(pass, n)
	}
	return false
}

func helperWaits(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return false
	}
	body := helperBody(pass, fn)
	if body == nil {
		return false
	}
	waits := false
	ast.Inspect(body, func(n ast.Node) bool {
		if waits {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if directWaits(pass, n) {
			waits = true
			return false
		}
		return true
	})
	return waits
}

func directWaits(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	case *ast.SendStmt:
		return true
	case *ast.RangeStmt:
		return isChanRange(pass, n)
	case *ast.CallExpr:
		return isSyncWait(pass, n)
	}
	return false
}

func helperDecl(pass *analysis.Pass, fn *types.Func) *ast.FuncDecl {
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if pass.TypesInfo.ObjectOf(fd.Name) == fn {
				return fd
			}
		}
	}
	return nil
}

func helperBody(pass *analysis.Pass, fn *types.Func) *ast.BlockStmt {
	if decl := helperDecl(pass, fn); decl != nil {
		return decl.Body
	}
	return nil
}

func isPackageVar(obj types.Object, pass *analysis.Pass) bool {
	return obj != nil && obj.Parent() == pass.Pkg.Scope()
}

func receiverVar(pass *analysis.Pass, decl *ast.FuncDecl) *types.Var {
	if decl.Recv == nil || len(decl.Recv.List) == 0 || len(decl.Recv.List[0].Names) == 0 {
		return nil
	}
	v, ok := pass.TypesInfo.ObjectOf(decl.Recv.List[0].Names[0]).(*types.Var)
	if !ok {
		return nil
	}
	return v
}

func paramVars(pass *analysis.Pass, decl *ast.FuncDecl) []*types.Var {
	var out []*types.Var
	if decl.Type == nil || decl.Type.Params == nil {
		return out
	}
	for _, field := range decl.Type.Params.List {
		for _, name := range field.Names {
			if v, ok := pass.TypesInfo.ObjectOf(name).(*types.Var); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

func callReceiverKey(pass *analysis.Pass, call *ast.CallExpr) flow.Key {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return flow.Key{}
	}
	k, _ := flow.KeyOf(pass.TypesInfo, sel.X)
	return k
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "." + b
}

func spinsOnLoad(pass *analysis.Pass, stmt *ast.ForStmt) bool {
	if stmt.Init != nil || stmt.Post != nil || stmt.Cond == nil {
		return false
	}
	return isLoadWaitCond(pass, stmt.Cond)
}

var loadCompareTokens = map[token.Token]bool{
	token.EQL: true,
	token.NEQ: true,
	token.LSS: true,
	token.LEQ: true,
	token.GTR: true,
	token.GEQ: true,
}

func isLoadWaitCond(pass *analysis.Pass, cond ast.Expr) bool {
	switch n := ast.Unparen(cond).(type) {
	case *ast.CallExpr:
		return isLoadCall(pass, n)
	case *ast.UnaryExpr:
		return isNegatedLoad(pass, n)
	case *ast.BinaryExpr:
		return isComparedLoad(pass, n)
	}
	return false
}

func isLoadCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isAtomicLoad(pass, call)
}

func isNegatedLoad(pass *analysis.Pass, n *ast.UnaryExpr) bool {
	if n.Op != token.NOT {
		return false
	}
	call, ok := ast.Unparen(n.X).(*ast.CallExpr)
	return ok && isAtomicLoad(pass, call)
}

func isComparedLoad(pass *analysis.Pass, n *ast.BinaryExpr) bool {
	if !loadCompareTokens[n.Op] {
		return false
	}
	left, lok := ast.Unparen(n.X).(*ast.CallExpr)
	right, rok := ast.Unparen(n.Y).(*ast.CallExpr)
	if lok && isAtomicLoad(pass, left) && isConst(pass, n.Y) {
		return true
	}
	return rok && isAtomicLoad(pass, right) && isConst(pass, n.X)
}

func isConst(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.Value != nil
}

func isAtomicLoad(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() != "sync/atomic" {
		return false
	}
	return fn.Name() == "Load" || strings.HasPrefix(fn.Name(), "Load")
}

func isChanRange(pass *analysis.Pass, n *ast.RangeStmt) bool {
	t := pass.TypesInfo.TypeOf(n.X)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func isSyncWait(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock", "(*sync.RWMutex).RLock", "(*sync.WaitGroup).Wait", "(*sync.Cond).Wait":
		return true
	}
	return false
}

func sortedDoneKeys(plain, deferred map[flow.Key][]token.Pos) []flow.Key {
	merged := map[flow.Key]struct{}{}
	for k := range plain {
		merged[k] = struct{}{}
	}
	for k := range deferred {
		merged[k] = struct{}{}
	}
	keys := slices.Collect(maps.Keys(merged))
	slices.SortFunc(keys, func(a, b flow.Key) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(keyName(a), keyName(b)), cmp.Compare(rootPos(a), rootPos(b)))
	})
	return keys
}

func keyName(k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	return k.Root.Name()
}

func rootPos(k flow.Key) token.Pos {
	if k.Root == nil {
		return token.NoPos
	}
	return k.Root.Pos()
}

func reportLateAdds(pass *analysis.Pass, group lateGroup, k flow.Key) {
	after := group.list[group.goIdx+1:]
	for _, s := range after {
		call, ok := directCall(s)
		if !ok {
			continue
		}
		ak, text, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok || ak != k {
			continue
		}
		if group.seen[call.Pos()] {
			continue
		}
		group.seen[call.Pos()] = true
		pass.Reportf(call.Pos(), "this Add runs after the goroutine it counts has started, so its Done can run first and panic with a negative WaitGroup counter, or a Wait before this Add can return early: call %s.Add before the go statement", text)
	}
}

func donePositions(pass *analysis.Pass, lit *ast.FuncLit) (plain, deferred map[flow.Key][]token.Pos) {
	plain = map[flow.Key][]token.Pos{}
	deferred = map[flow.Key][]token.Pos{}
	for _, s := range lit.Body.List {
		if d, ok := unwrap(s).(*ast.DeferStmt); ok {
			collectDonesIn(pass, d, deferred)
		} else {
			collectDonesIn(pass, s, plain)
		}
	}
	return plain, deferred
}

func collectDonesIn(pass *analysis.Pass, n ast.Node, out map[flow.Key][]token.Pos) {
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if k, _, ok := wgKey(pass.TypesInfo, call, "Done"); ok {
			out[k] = append(out[k], call.Pos())
		}
		return true
	})
}

func hasAddBefore(first map[flow.Key]token.Pos, g *ast.GoStmt, k flow.Key) bool {
	p, ok := first[k]
	return ok && p < g.Pos()
}
