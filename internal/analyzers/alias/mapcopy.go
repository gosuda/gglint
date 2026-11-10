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
	m    *types.Var
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
		switch n := n.(type) {
		case *ast.AssignStmt:
			if d, ok := mapAssignDef(info, n); ok {
				out = append(out, d)
			}
		case *ast.ValueSpec:
			if d, ok := mapValueDef(info, n); ok {
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
	m, text, ok := mapIndex(info, st.Rhs[0])
	if !ok {
		return mapDef{}, false
	}
	return mapDef{v: v, m: m, text: text, pos: st.Pos(), stmt: st}, true
}

func mapValueDef(info *types.Info, sp *ast.ValueSpec) (mapDef, bool) {
	if len(sp.Names) != 1 || len(sp.Values) != 1 || sp.Names[0].Name == "_" {
		return mapDef{}, false
	}
	v, ok := info.Defs[sp.Names[0]].(*types.Var)
	if !ok {
		return mapDef{}, false
	}
	m, text, ok := mapIndex(info, sp.Values[0])
	if !ok {
		return mapDef{}, false
	}
	return mapDef{v: v, m: m, text: text, pos: sp.Pos(), stmt: sp}, true
}

func mapIndex(info *types.Info, e ast.Expr) (*types.Var, string, bool) {
	var x ast.Expr
	switch n := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		x = n.X
	case *ast.IndexListExpr:
		x = n.X
	default:
		return nil, "", false
	}
	m, ok := identVar(info, x)
	if !ok {
		return nil, "", false
	}
	t := info.TypeOf(x)
	if t == nil {
		return nil, "", false
	}
	mp, ok := t.Underlying().(*types.Map)
	if !ok {
		return nil, "", false
	}
	if _, ok := mp.Elem().Underlying().(*types.Struct); !ok {
		return nil, "", false
	}
	return m, types.ExprString(x), true
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
			checkMapAssign(pass, fn, n, d)
		case *ast.IncDecStmt:
			checkMapIncDec(pass, fn, n, d)
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
			found = usesVar(info, n, d.v)
		case *ast.CallExpr:
			found = argsUseVar(info, n, d.v)
		case *ast.AssignStmt:
			found = rhsUsesVar(info, n, d)
		}
		return !found
	})
	return found
}

func argsUseVar(info *types.Info, call *ast.CallExpr, v *types.Var) bool {
	for _, a := range call.Args {
		if usesVar(info, a, v) {
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
		if usesVar(info, rhs, d.v) {
			return true
		}
	}
	return false
}

func checkMapAssign(pass *analysis.Pass, fn ast.Node, st *ast.AssignStmt, d mapDef) {
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

func checkMapIncDec(pass *analysis.Pass, fn ast.Node, st *ast.IncDecStmt, d mapDef) {
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
