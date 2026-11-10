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
	Doc:      "report a WaitGroup Add that is a plain statement after a go statement whose function literal calls Done, in the same statement list; an Add earlier in the function silences it, also an Add inside a function literal that is called before the go statement directly or through a local variable, even when that variable may hold another function by then; an Add in a go or defer statement does not; a literal that receives, ranges over a channel, or locks or waits is not reported, because that wait can order the Add",
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

type varLitSet struct {
	lits  map[*types.Var][]*ast.FuncLit
	calls map[*types.Var]token.Pos
}

func newVarLitSet() *varLitSet {
	return &varLitSet{lits: map[*types.Var][]*ast.FuncLit{}, calls: map[*types.Var]token.Pos{}}
}

func collectVarLits(info *types.Info, body *ast.BlockStmt, set *varLitSet) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			noteAssignLit(info, n, set)
		case *ast.ValueSpec:
			noteValueLit(info, n, set)
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		case *ast.CallExpr:
			noteVarCall(info, n, set.calls)
			return true
		}
		return true
	})
}

func applyVarLits(pass *analysis.Pass, fn ast.Node, set *varLitSet, out map[flow.Key]token.Pos) {
	for v, lits := range set.lits {
		applyOneVarLit(pass, fn, v, lits, set, out)
	}
}

func applyOneVarLit(pass *analysis.Pass, fn ast.Node, v *types.Var, lits []*ast.FuncLit, set *varLitSet, out map[flow.Key]token.Pos) {
	if !flow.IsLocal(pass.TypesInfo, fn, v) {
		return
	}
	cp, ok := set.calls[v]
	if !ok {
		return
	}
	seen := map[flow.Key]bool{}
	for _, lit := range lits {
		for _, k := range litAddKeys(pass, lit) {
			if seen[k] {
				continue
			}
			seen[k] = true
			if p, dup := out[k]; !dup || cp < p {
				out[k] = cp
			}
		}
	}
}

func noteAssignLit(info *types.Info, s *ast.AssignStmt, set *varLitSet) {
	for i, lhs := range s.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := info.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		if len(s.Lhs) == 1 && len(s.Rhs) == 1 && i == 0 {
			if lit, ok := ast.Unparen(s.Rhs[0]).(*ast.FuncLit); ok {
				set.lits[v] = append(set.lits[v], lit)
			}
		}
	}
}

func noteValueLit(info *types.Info, s *ast.ValueSpec, set *varLitSet) {
	if len(s.Values) == 0 {
		return
	}
	for i, name := range s.Names {
		v, ok := info.ObjectOf(name).(*types.Var)
		if !ok {
			continue
		}
		if len(s.Names) == 1 && len(s.Values) == 1 && i == 0 {
			if lit, ok := ast.Unparen(s.Values[0]).(*ast.FuncLit); ok {
				set.lits[v] = append(set.lits[v], lit)
			}
		}
	}
}

func noteVarCall(info *types.Info, call *ast.CallExpr, calls map[*types.Var]token.Pos) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	if p, dup := calls[v]; !dup || call.Pos() < p {
		calls[v] = call.Pos()
	}
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
