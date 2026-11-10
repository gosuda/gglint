package alias

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func inPlaceSilent(pass *analysis.Pass, fn ast.Node, b *types.Var, def sliceDef) bool {
	info := pass.TypesInfo
	if !def.highKnown || def.high != 0 || !def.zeroLow {
		return false
	}
	appends := appendsTo(info, fn, b)
	if len(appends) == 0 {
		return false
	}
	for _, st := range appends {
		if !singleInReadingLoop(info, fn, st, def.a) {
			return false
		}
	}
	if !atMostOnePerIteration(pass, fn, appends) {
		return false
	}
	return !hasElementReadAfterLoop(pass, fn, def.a, b, appends)
}

func hasElementReadAfterLoop(pass *analysis.Pass, fn ast.Node, a, b *types.Var, appends []*ast.AssignStmt) bool {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return true
	}
	writes := lhsIdents(fn)
	skip := lenCapIdents(pass.TypesInfo, fn, a)
	stores := storeBases(pass.TypesInfo, fn, a)
	tails := tailSlices(pass.TypesInfo, fn, a, b)
	for _, st := range appends {
		loop := enclosingLoop(fn, st)
		if loop == nil {
			return true
		}
		scan := afterLoopScan{pass: pass, fn: fn, graph: g, writes: writes, skip: skip, stores: stores, tails: tails, target: a, from: st, loop: loop}
		if scan.found() {
			return true
		}
	}
	return false
}

type afterLoopScan struct {
	pass   *analysis.Pass
	fn     ast.Node
	graph  *flow.Graph
	writes map[*ast.Ident]bool
	skip   map[*ast.Ident]bool
	stores map[*ast.Ident]bool
	tails  map[*ast.Ident]bool
	target *types.Var
	from   *ast.AssignStmt
	loop   ast.Node
}

func (s afterLoopScan) found() bool {
	hit := false
	ast.Inspect(flow.Body(s.fn), func(n ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || !s.isRead(id) {
			return true
		}
		if s.reaches(id) {
			hit = true
		}
		return !hit
	})
	return hit
}

func (s afterLoopScan) isRead(id *ast.Ident) bool {
	if id.Pos() <= s.loop.End() || s.skip[id] || s.writes[id] || s.stores[id] || s.tails[id] {
		return false
	}
	return s.pass.TypesInfo.Uses[id] == s.target
}

func storeBases(info *types.Info, fn ast.Node, a *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range st.Lhs {
			markLHSIDs(info, lhs, a, out)
		}
		return true
	})
	return out
}

func markLHSIDs(info *types.Info, lhs ast.Expr, a *types.Var, out map[*ast.Ident]bool) {
	ast.Inspect(lhs, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok {
			if v, ok := varOf(info, id); ok && v == a {
				out[id] = true
			}
		}
		return true
	})
}

func tailSlices(info *types.Info, fn ast.Node, a, b *types.Var) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		s, ok := n.(*ast.SliceExpr)
		if !ok {
			return true
		}
		id, ok := ast.Unparen(s.X).(*ast.Ident)
		if !ok {
			return true
		}
		if v, ok := varOf(info, id); !ok || v != a {
			return true
		}
		if isLenOfTarget(info, s.Low, b) {
			out[id] = true
		}
		return true
	})
	return out
}

func isLenOfTarget(info *types.Info, e ast.Expr, target *types.Var) bool {
	if e == nil {
		return false
	}
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	blt, ok := info.Uses[id].(*types.Builtin)
	if !ok || blt.Name() != lenName {
		return false
	}
	v, ok := identVar(info, call.Args[0])
	return ok && v == target
}

func (s afterLoopScan) reaches(to ast.Node) bool {
	return reachesForCap(s.graph, s.pass.TypesInfo, s.target, s.from, to)
}
