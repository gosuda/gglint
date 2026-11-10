package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func runMapcopy(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkMapcopy(pass, fn)
	})
	return nil, nil
}

type mapDef struct {
	v    *types.Var
	text string
	pos  token.Pos
	stmt ast.Node
}

func checkMapcopy(pass *analysis.Pass, fn ast.Node) {
	defs := collectMapDefs(pass.TypesInfo, fn)
	if len(defs) == 0 {
		return
	}
	used := collectWholeUses(pass.TypesInfo, fn)
	writes := collectFieldWrites(pass.TypesInfo, fn)
	for _, d := range defs {
		if used[d.v] {
			continue
		}
		for _, st := range writes[d.v] {
			if st.Pos() > d.pos {
				reportMapCopy(pass, st, d)
			}
		}
	}
}

func collectMapDefs(info *types.Info, fn ast.Node) []mapDef {
	var out []mapDef
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok {
			if d, ok := mapAssignDef(info, st); ok {
				out = append(out, d)
			}
		}
		return true
	})
	return out
}

func mapAssignDef(info *types.Info, st *ast.AssignStmt) (mapDef, bool) {
	if st.Tok != token.DEFINE || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return mapDef{}, false
	}
	id, ok := ast.Unparen(st.Lhs[0]).(*ast.Ident)
	if !ok || id.Name == "_" {
		return mapDef{}, false
	}
	v, ok := info.Defs[id].(*types.Var)
	if !ok {
		return mapDef{}, false
	}
	text, ok := mapIndex(info, st.Rhs[0])
	if !ok {
		return mapDef{}, false
	}
	return mapDef{v: v, text: text, pos: st.Pos(), stmt: st}, true
}

func mapIndex(info *types.Info, e ast.Expr) (string, bool) {
	var x ast.Expr
	switch n := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		x = n.X
	case *ast.IndexListExpr:
		x = n.X
	default:
		return "", false
	}
	t := info.TypeOf(x)
	if t == nil {
		return "", false
	}
	mp, ok := t.Underlying().(*types.Map)
	if !ok {
		return "", false
	}
	if _, ok := mp.Elem().Underlying().(*types.Struct); !ok {
		return "", false
	}
	return types.ExprString(x), true
}

func collectWholeUses(info *types.Info, fn ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				markWholeUses(info, r, out)
			}
		case *ast.CallExpr:
			for _, a := range n.Args {
				markWholeUses(info, a, out)
			}
		case *ast.AssignStmt:
			for _, rhs := range n.Rhs {
				markWholeUses(info, rhs, out)
			}
		}
		return true
	})
	return out
}

func markWholeUses(info *types.Info, e ast.Expr, out map[*types.Var]bool) {
	var stack []ast.Node
	ast.Inspect(e, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok && !isFieldBase(stack, id) {
				out[v] = true
			}
		}
		stack = append(stack, n)
		return true
	})
}

func collectFieldWrites(info *types.Info, fn ast.Node) map[*types.Var][]ast.Stmt {
	out := map[*types.Var][]ast.Stmt{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			if v, ok := assignFieldRoot(info, n); ok {
				out[v] = append(out[v], n)
			}
		case *ast.IncDecStmt:
			if v, ok := fieldRoot(info, n.X); ok {
				out[v] = append(out[v], n)
			}
		}
		return true
	})
	return out
}

func assignFieldRoot(info *types.Info, st *ast.AssignStmt) (*types.Var, bool) {
	for _, lhs := range st.Lhs {
		if v, ok := fieldRoot(info, lhs); ok {
			return v, true
		}
	}
	return nil, false
}

func fieldRoot(info *types.Info, e ast.Expr) (*types.Var, bool) {
	if passesPointer(info, e) {
		return nil, false
	}
	k, ok := flow.KeyOf(info, e)
	if !ok || k.Path == "" {
		return nil, false
	}
	v, ok := k.Root.(*types.Var)
	return v, ok
}

func isFieldBase(stack []ast.Node, id *ast.Ident) bool {
	child := ast.Node(id)
	rest := stack
	for len(rest) > 0 {
		top := rest[len(rest)-1]
		par, ok := top.(*ast.ParenExpr)
		if !ok || par.X != child {
			break
		}
		child = par
		rest = rest[:len(rest)-1]
	}
	if len(rest) == 0 {
		return false
	}
	sel, ok := rest[len(rest)-1].(*ast.SelectorExpr)
	return ok && sel.X == child
}

func reportMapCopy(pass *analysis.Pass, st ast.Stmt, d mapDef) {
	pass.Reportf(st.Pos(), "%s is a copy of the element of %s, so this write is lost: store %s back into the map", d.v.Name(), d.text, d.v.Name())
}
