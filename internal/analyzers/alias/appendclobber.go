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
}

func checkAppendclobber(pass *analysis.Pass, fn ast.Node) {
	defs := collectSliceDefs(pass.TypesInfo, fn)
	if len(defs) == 0 {
		return
	}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok {
			checkAppendAssign(pass, fn, st, defs)
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
	defs[b] = sliceDef{a: a, pos: pos, high: high, highKnown: highKnown}
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

func checkAppendAssign(pass *analysis.Pass, fn ast.Node, st *ast.AssignStmt, defs map[*types.Var]sliceDef) {
	b, def, call, ok := appendDef(pass.TypesInfo, st, defs)
	if !ok {
		return
	}
	if reallocates(pass.TypesInfo, fn, def, call) {
		return
	}
	if assignedBetween(pass.TypesInfo, fn, b, def.pos, st.Pos()) {
		return
	}
	if assignedBetween(pass.TypesInfo, fn, def.a, def.pos, st.Pos()) {
		return
	}
	if !mentionedAfter(pass, fn, def.a, st, st.End()) {
		return
	}
	pass.Reportf(call.Pos(), "%s shares its backing array with %s, so this append overwrites elements of %s that are read later: slice it with three indexes to cap its capacity", b.Name(), def.a.Name(), def.a.Name())
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

func reallocates(info *types.Info, fn ast.Node, def sliceDef, call *ast.CallExpr) bool {
	if !def.highKnown {
		return false
	}
	if call.Ellipsis.IsValid() {
		capacity, capKnown := arrayLenCap(info, fn, def.a, def.pos)
		return capKnown && def.high == capacity
	}
	n := len(call.Args) - 1
	capacity, capKnown := arrayLenCap(info, fn, def.a, def.pos)
	return capKnown && def.high+n > capacity
}

func arrayLenCap(info *types.Info, fn ast.Node, a *types.Var, before token.Pos) (capacity int, capKnown bool) {
	best := token.NoPos
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		c, ck, ok := defBefore(info, st, a, before, best)
		if !ok {
			return true
		}
		best, capacity, capKnown = st.Pos(), c, ck
		return true
	})
	if best == token.NoPos {
		return 0, false
	}
	if reassignedBefore(info, fn, a, before, best) {
		return 0, false
	}
	return capacity, capKnown
}

func reassignedBefore(info *types.Info, fn ast.Node, a *types.Var, before, best token.Pos) bool {
	hit := false
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok || st.Pos() == best || st.Pos() >= before {
			return true
		}
		hit = assignsVar(info, st, a)
		return !hit
	})
	return hit
}

func assignsVar(info *types.Info, st *ast.AssignStmt, a *types.Var) bool {
	for _, lhs := range st.Lhs {
		if isVarTarget(info, lhs, a) {
			return true
		}
	}
	return false
}

func defBefore(info *types.Info, st *ast.AssignStmt, a *types.Var, before, best token.Pos) (capacity int, capKnown, ok bool) {
	if !isDefOf(info, st, a) {
		return 0, false, false
	}
	if st.Pos() >= before || st.Pos() <= best {
		return 0, false, false
	}
	c, ck := defLenCap(info, st.Rhs[0])
	if !ck {
		return 0, false, false
	}
	return c, ck, true
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

func mentionedAfter(pass *analysis.Pass, fn ast.Node, v *types.Var, from ast.Node, after token.Pos) bool {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return false
	}
	writes := lhsIdents(fn)
	found := false
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if isReadMention(pass.TypesInfo, writes, id, v, after) && g.Reaches(from, id, nil) {
			found = true
		}
		return !found
	})
	return found
}

func isReadMention(info *types.Info, writes map[*ast.Ident]bool, id *ast.Ident, v *types.Var, after token.Pos) bool {
	return id.Pos() > after && info.Uses[id] == v && !writes[id]
}

func lhsIdents(fn ast.Node) map[*ast.Ident]bool {
	out := map[*ast.Ident]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if st.Tok != token.ASSIGN && st.Tok != token.DEFINE {
			return true
		}
		for _, lhs := range st.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
				out[id] = true
			}
		}
		return true
	})
	return out
}
