package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

type indexedUse struct {
	pos token.Pos
	cur inspector.Cursor
}

type funcIndex struct {
	uses      map[*types.Var][]indexedUse
	assigns   map[*types.Var][]token.Pos
	nilChecks map[*types.Var][]token.Pos
}

func buildFuncIndex(pass *analysis.Pass, fn inspector.Cursor) *funcIndex {
	idx := &funcIndex{
		uses:      map[*types.Var][]indexedUse{},
		assigns:   map[*types.Var][]token.Pos{},
		nilChecks: map[*types.Var][]token.Pos{},
	}
	collectUses(pass, fn, idx)
	collectAssigns(pass, fn, idx)
	collectNilChecks(pass, fn, idx)
	return idx
}

func collectUses(pass *analysis.Pass, fn inspector.Cursor, idx *funcIndex) {
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
			idx.uses[v] = append(idx.uses[v], indexedUse{pos: id.Pos(), cur: cur})
		}
	}
}

func collectAssigns(pass *analysis.Pass, fn inspector.Cursor, idx *funcIndex) {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		for _, v := range assignedVars(pass, cur.Node()) {
			idx.assigns[v] = append(idx.assigns[v], cur.Node().Pos())
		}
	}
}

func collectNilChecks(pass *analysis.Pass, fn inspector.Cursor, idx *funcIndex) {
	for cur := range fn.Preorder((*ast.BinaryExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		b := inspection.As[*ast.BinaryExpr](cur.Node())
		if b.Op != token.EQL && b.Op != token.NEQ {
			continue
		}
		if v := nilComparedVar(pass, b); v != nil {
			idx.nilChecks[v] = append(idx.nilChecks[v], b.Pos())
		}
	}
}

func assignedVars(pass *analysis.Pass, n ast.Node) []*types.Var {
	switch t := n.(type) {
	case *ast.AssignStmt:
		return assignStmtVars(pass, t)
	case *ast.IncDecStmt:
		return incDecVars(pass, t)
	case *ast.RangeStmt:
		return rangeStmtVars(pass, t)
	}
	return nil
}

func assignStmtVars(pass *analysis.Pass, t *ast.AssignStmt) []*types.Var {
	var out []*types.Var
	for _, lhs := range t.Lhs {
		if v := varOfIdent(pass, lhs); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func incDecVars(pass *analysis.Pass, t *ast.IncDecStmt) []*types.Var {
	if v := varOfIdent(pass, t.X); v != nil {
		return []*types.Var{v}
	}
	return nil
}

func rangeStmtVars(pass *analysis.Pass, t *ast.RangeStmt) []*types.Var {
	if t.Tok != token.ASSIGN {
		return nil
	}
	var out []*types.Var
	for _, e := range []ast.Expr{t.Key, t.Value} {
		if e == nil {
			continue
		}
		if v := varOfIdent(pass, e); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func varOfIdent(pass *analysis.Pass, e ast.Expr) *types.Var {
	for {
		switch t := ast.Unparen(e).(type) {
		case *ast.Ident:
			if v, ok := pass.TypesInfo.ObjectOf(t).(*types.Var); ok {
				return v
			}
			return nil
		case *ast.SelectorExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.StarExpr:
			e = t.X
		default:
			return nil
		}
	}
}

func nilComparedVar(pass *analysis.Pass, b *ast.BinaryExpr) *types.Var {
	if v := varIfNilOther(pass, b.X, b.Y); v != nil {
		return v
	}
	return varIfNilOther(pass, b.Y, b.X)
}

func varIfNilOther(pass *analysis.Pass, side, other ast.Expr) *types.Var {
	id, ok := ast.Unparen(side).(*ast.Ident)
	if !ok {
		return nil
	}
	if !isNilIdent(pass.TypesInfo, other) {
		return nil
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return v
	}
	return nil
}

func (idx *funcIndex) firstUse(v *types.Var, after token.Pos) (inspector.Cursor, bool) {
	list := idx.uses[v]
	i := sort.Search(len(list), func(i int) bool { return list[i].pos > after })
	if i >= len(list) {
		return inspector.Cursor{}, false
	}
	return list[i].cur, true
}

func (idx *funcIndex) hasAssignIn(v *types.Var, from, to token.Pos) bool {
	return hasPosIn(idx.assigns[v], from, to)
}

func (idx *funcIndex) hasNilCheckIn(v *types.Var, from, to token.Pos) bool {
	return hasPosIn(idx.nilChecks[v], from, to)
}

func hasPosIn(list []token.Pos, from, to token.Pos) bool {
	i := sort.Search(len(list), func(i int) bool { return list[i] > from })
	return i < len(list) && list[i] < to
}
