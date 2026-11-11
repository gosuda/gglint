package alias

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type readSite struct {
	from  ast.Node
	after token.Pos
	bound int
	end   int
}

func overwrittenReadAfter(scope *appendScope, a *types.Var, site readSite) bool {
	if scope.graph == nil {
		return false
	}
	kills := scope.assigns[a]
	if scopeIndexHitStores(scope, a, site, kills) {
		return true
	}
	if scopeSliceHitStores(scope, a, site, kills) {
		return true
	}
	return scopeWholeHitStores(scope, a, site, kills)
}

func scopeIndexHitStores(scope *appendScope, a *types.Var, site readSite, kills []ast.Node) bool {
	for _, t := range scope.indexReads[a] {
		if t.Pos() <= site.after {
			continue
		}
		k, ok := constInt(scope.info, t.Index)
		if !ok || k < site.bound || k >= site.end {
			continue
		}
		if scope.graph.Reaches(site.from, t, storeAvoid(scope, a, kills, k)) {
			return true
		}
	}
	return false
}

func scopeSliceHitStores(scope *appendScope, a *types.Var, site readSite, kills []ast.Node) bool {
	for _, t := range scope.sliceReads[a] {
		if t.Pos() <= site.after {
			continue
		}
		if scopeSliceOutside(scope.info, t, site.bound, site.end) {
			continue
		}
		if sliceHasLiveIndex(scope, a, site, kills, t) {
			return true
		}
	}
	return false
}

func scopeWholeHitStores(scope *appendScope, a *types.Var, site readSite, kills []ast.Node) bool {
	for _, id := range scope.wholeReads[a] {
		if id.Pos() <= site.after {
			continue
		}
		if wholeHasLiveIndex(scope, a, site, kills, id) {
			return true
		}
	}
	return false
}

func storeAvoid(scope *appendScope, a *types.Var, kills []ast.Node, k int) func(ast.Node) bool {
	return func(n ast.Node) bool {
		if slices.Contains(kills, n) {
			return true
		}
		return isElementStoreTo(scope.info, scope, n, a, k)
	}
}

func sliceHasLiveIndex(scope *appendScope, a *types.Var, site readSite, kills []ast.Node, t *ast.SliceExpr) bool {
	if site.end-site.bound > 64 {
		return scope.graph.Reaches(site.from, t, wholeAvoid(kills))
	}
	for k := site.bound; k < site.end; k++ {
		if !sliceCovers(scope.info, t, k) {
			continue
		}
		if scope.graph.Reaches(site.from, t, storeAvoid(scope, a, kills, k)) {
			return true
		}
	}
	return false
}

func wholeHasLiveIndex(scope *appendScope, a *types.Var, site readSite, kills []ast.Node, id *ast.Ident) bool {
	if site.end-site.bound > 64 {
		return scope.graph.Reaches(site.from, id, wholeAvoid(kills))
	}
	for k := site.bound; k < site.end; k++ {
		if scope.graph.Reaches(site.from, id, storeAvoid(scope, a, kills, k)) {
			return true
		}
	}
	return false
}

func wholeAvoid(kills []ast.Node) func(ast.Node) bool {
	return func(n ast.Node) bool {
		return slices.Contains(kills, n)
	}
}

func sliceCovers(info *types.Info, s *ast.SliceExpr, k int) bool {
	low := 0
	if s.Low != nil {
		v, ok := constInt(info, s.Low)
		if !ok {
			return true
		}
		low = v
	}
	if k < low {
		return false
	}
	if s.High != nil {
		h, ok := constInt(info, s.High)
		if !ok {
			return true
		}
		return k < h
	}
	return true
}

func isElementStoreTo(info *types.Info, scope *appendScope, n ast.Node, a *types.Var, k int) bool {
	st, ok := n.(*ast.AssignStmt)
	if !ok || st.Tok != token.ASSIGN {
		return false
	}
	for _, lhs := range st.Lhs {
		if lhsStoreToK(info, scope, lhs, a, k) {
			return true
		}
	}
	return false
}

func lhsStoreToK(info *types.Info, scope *appendScope, lhs ast.Expr, a *types.Var, k int) bool {
	idx, ok := ast.Unparen(lhs).(*ast.IndexExpr)
	if !ok {
		return false
	}
	v, _, ok := resolveIndexBase(info, scope.wanted, scope.ptrs, idx.X)
	if !ok || v != a {
		return false
	}
	c, ok := constInt(info, idx.Index)
	return ok && c == k
}

func scopeSliceOutside(info *types.Info, s *ast.SliceExpr, bound, end int) bool {
	low := 0
	if s.Low != nil {
		v, ok := constInt(info, s.Low)
		if !ok {
			return true
		}
		low = v
	}
	if s.High != nil {
		h, ok := constInt(info, s.High)
		if !ok {
			return true
		}
		if h <= bound {
			return true
		}
		return low >= end
	}
	if s.Max != nil {
		if _, ok := constInt(info, s.Max); !ok {
			return true
		}
	}
	return low >= end
}

func isCopyCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "copy"
}

func copyDestNodes(info *types.Info, fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isCopyCall(info, call) || len(call.Args) == 0 {
			return true
		}
		out[ast.Unparen(call.Args[0])] = true
		return true
	})
	return out
}

func clearSliceNodes(info *types.Info, fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isClearCall(info, call) {
			return true
		}
		for _, arg := range call.Args {
			out[ast.Unparen(arg)] = true
		}
		return true
	})
	return out
}

func plainStoreBases(s *appendScope, a *types.Var) map[*ast.Ident]bool {
	scan := storeScan{info: s.info, fn: s.fn, target: a, wanted: s.wanted, ptrs: s.ptrs, out: map[*ast.Ident]bool{}}
	scan.collect()
	return scan.out
}

type storeScan struct {
	info   *types.Info
	fn     ast.Node
	target *types.Var
	wanted map[*types.Var]bool
	ptrs   map[*types.Var]*types.Var
	out    map[*ast.Ident]bool
}

func (s storeScan) collect() {
	ast.Inspect(flow.Body(s.fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok || st.Tok != token.ASSIGN {
			return true
		}
		s.mark(st)
		return true
	})
}

func (s storeScan) mark(st *ast.AssignStmt) {
	for _, lhs := range st.Lhs {
		idx, ok := ast.Unparen(lhs).(*ast.IndexExpr)
		if !ok {
			continue
		}
		v, id, ok := resolveIndexBase(s.info, s.wanted, s.ptrs, idx.X)
		if !ok || v != s.target {
			continue
		}
		s.out[id] = true
	}
}

func clearArgIdents(info *types.Info, fn ast.Node, a *types.Var) map[*ast.Ident]bool {
	scan := clearScan{info: info, fn: fn, target: a, out: map[*ast.Ident]bool{}}
	scan.collect()
	return scan.out
}

type clearScan struct {
	info   *types.Info
	fn     ast.Node
	target *types.Var
	out    map[*ast.Ident]bool
}

func (s clearScan) collect() {
	ast.Inspect(flow.Body(s.fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isClearCall(s.info, call) {
			return true
		}
		for _, arg := range call.Args {
			ast.Inspect(arg, func(x ast.Node) bool {
				s.mark(x)
				return true
			})
		}
		return true
	})
}

func (s clearScan) mark(x ast.Node) {
	id, ok := x.(*ast.Ident)
	if !ok {
		return
	}
	v, ok := varOf(s.info, id)
	if !ok || v != s.target {
		return
	}
	s.out[id] = true
}

func isClearCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "clear"
}

func hasCoveringIndex(pass *analysis.Pass, fn ast.Node, a *types.Var, from ast.Node, threshold int) bool {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return false
	}
	avoid := func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		return ok && assignsVar(pass.TypesInfo, st, a)
	}
	found := false
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		idx, ok := n.(*ast.IndexExpr)
		if !ok || !indexOf(pass.TypesInfo, idx, a, threshold) {
			return true
		}
		if idx.Pos() <= from.End() {
			return true
		}
		if g.Reaches(from, idx, avoid) {
			found = true
		}
		return !found
	})
	return found
}
