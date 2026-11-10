package alias

import (
	"go/ast"
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
	a   *types.Var
	pos token.Pos
}

func checkAppendclobber(pass *analysis.Pass, fn ast.Node) {
	defs := collectSliceDefs(pass.TypesInfo, fn)
	if len(defs) == 0 {
		return
	}
	ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
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
	ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
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
	defs[b] = sliceDef{a: a, pos: pos}
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
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return
	}
	call, ok := isAppend(pass.TypesInfo, st.Rhs[0])
	if !ok || len(call.Args) < 2 {
		return
	}
	b, ok := appendTarget(pass.TypesInfo, st, call)
	if !ok {
		return
	}
	def, ok := defs[b]
	if !ok {
		return
	}
	if assignedBetween(pass.TypesInfo, fn, b, def.pos, st.Pos()) {
		return
	}
	if !mentionedAfter(pass.TypesInfo, fn, def.a, st.End()) {
		return
	}
	pass.Reportf(call.Pos(), "%s shares its backing array with %s, so this append overwrites elements of %s that are read later: slice it with three indexes to cap its capacity", b.Name(), def.a.Name(), def.a.Name())
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
	ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
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

func mentionedAfter(info *types.Info, fn ast.Node, v *types.Var, after token.Pos) bool {
	found := false
	ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
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
		if id.Pos() > after && info.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}
