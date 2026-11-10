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
	for _, d := range collectMapDefs(pass.TypesInfo, fn) {
		checkMapWrites(pass, fn, d)
	}
}

func collectMapDefs(info *types.Info, fn ast.Node) []mapDef {
	var out []mapDef
	ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
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

func checkMapWrites(pass *analysis.Pass, fn ast.Node, d mapDef) {
	if copiedElsewhere(pass.TypesInfo, fn, d) {
		return
	}
	ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			checkMapAssign(pass, n, d)
		case *ast.IncDecStmt:
			checkMapIncDec(pass, n, d)
		}
		return true
	})
}

func copiedElsewhere(info *types.Info, fn ast.Node, d mapDef) bool {
	body := bodyOf(fn)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.ReturnStmt:
			found = resultsUseVar(info, n, d.v)
		case *ast.CallExpr:
			found = argsUseVar(info, n, d.v)
		case *ast.AssignStmt:
			found = rhsUsesVar(info, n, d)
		}
		return !found
	})
	return found
}

func resultsUseVar(info *types.Info, st *ast.ReturnStmt, v *types.Var) bool {
	for _, r := range st.Results {
		if exprUsesVar(info, r, v) {
			return true
		}
	}
	return false
}

func exprUsesVar(info *types.Info, e ast.Expr, v *types.Var) bool {
	found := false
	var stack []ast.Node
	ast.Inspect(e, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok {
			if info.Uses[id] == v && !isFieldBase(stack, id) {
				found = true
				return false
			}
		}
		stack = append(stack, n)
		return true
	})
	return found
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

func argsUseVar(info *types.Info, call *ast.CallExpr, v *types.Var) bool {
	for _, a := range call.Args {
		if exprUsesVar(info, a, v) {
			return true
		}
	}
	return false
}

func rhsUsesVar(info *types.Info, st *ast.AssignStmt, d mapDef) bool {
	if st == d.stmt {
		return false
	}
	for _, rhs := range st.Rhs {
		if exprUsesVar(info, rhs, d.v) {
			return true
		}
	}
	return false
}

func checkMapAssign(pass *analysis.Pass, st *ast.AssignStmt, d mapDef) {
	if st.Pos() <= d.pos {
		return
	}
	for _, lhs := range st.Lhs {
		if fieldOf(pass.TypesInfo, lhs, d.v) {
			reportMapCopy(pass, st, d)
			return
		}
	}
}

func checkMapIncDec(pass *analysis.Pass, st *ast.IncDecStmt, d mapDef) {
	if st.Pos() <= d.pos {
		return
	}
	if fieldOf(pass.TypesInfo, st.X, d.v) {
		reportMapCopy(pass, st, d)
	}
}

func fieldOf(info *types.Info, e ast.Expr, v *types.Var) bool {
	k, ok := flow.KeyOf(info, e)
	return ok && k.Root == v && k.Path != ""
}

func reportMapCopy(pass *analysis.Pass, st ast.Stmt, d mapDef) {
	pass.Reportf(st.Pos(), "%s is a copy of the element of %s, so this write is lost: store %s back into the map", d.v.Name(), d.text, d.v.Name())
}
