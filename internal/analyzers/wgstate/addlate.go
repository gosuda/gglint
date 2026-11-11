package wgstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

var addlateAnalyzer = &analysis.Analyzer{
	Name:     "addlate",
	Doc:      "report a WaitGroup Add that is a plain statement after a go statement whose function literal calls Done, in the same statement list; an Add earlier in the function silences it, also an Add inside a function literal that is called before the go statement directly or through a local variable that can hold it at that call; an Add in a go or defer statement does not; a literal that receives, ranges over a channel, or locks or waits is not reported, because that wait can order the Add",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runAddlate,
}

type lateGroup struct {
	list  []ast.Stmt
	goIdx int
	seen  map[token.Pos]bool
}

func runAddlate(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkAddlate(pass, fn)
	})
	return nil, nil
}

func checkAddlate(pass *analysis.Pass, fn ast.Node) {
	first := firstAddPositions(pass, fn)
	eachList(fn, func(list []ast.Stmt) {
		seen := map[token.Pos]bool{}
		for i, s := range list {
			g, ok := unwrap(s).(*ast.GoStmt)
			if !ok {
				continue
			}
			checkGoAddlate(pass, g, list, i, seen, first)
		}
	})
}

func firstAddPositions(pass *analysis.Pass, fn ast.Node) map[flow.Key]token.Pos {
	out := map[flow.Key]token.Pos{}
	body := funcBody(fn)
	if body == nil {
		return out
	}
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
	mergeInPlaceAdds(pass, body, out)
	mergeVarLitAdds(pass, fn, body, out)
	return out
}

func litAddKeys(pass *analysis.Pass, lit *ast.FuncLit) []flow.Key {
	seen := map[flow.Key]bool{}
	var out []flow.Key
	if lit.Body == nil {
		return out
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
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
		for _, y := range b {
			if x == y {
				return true
			}
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

func checkGoAddlate(pass *analysis.Pass, g *ast.GoStmt, list []ast.Stmt, goIdx int, seen map[token.Pos]bool, first map[flow.Key]token.Pos) {
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
	group := lateGroup{list: list, goIdx: goIdx, seen: seen}
	for _, k := range sortedDoneKeys(plain, deferred) {
		if hasAddBefore(first, g, k) {
			continue
		}
		reportLateAdds(pass, group, k)
	}
}

func waitsForParent(pass *analysis.Pass, lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
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
	case *ast.RangeStmt:
		return isChanRange(pass, n)
	case *ast.CallExpr:
		return isSyncWait(pass, n)
	}
	return false
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
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(keyName(a), keyName(b)))
	})
	return keys
}

func keyName(k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	return k.Root.Name()
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
