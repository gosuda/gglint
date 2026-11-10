package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func runRangeref(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		findRanges(fn, func(rs *ast.RangeStmt) {
			checkRangeref(pass, rs)
		})
	})
	return nil, nil
}

func checkRangeref(pass *analysis.Pass, rs *ast.RangeStmt) {
	info := pass.TypesInfo
	v, ok := rangeValue(info, rs)
	if !ok || !rangeStructElem(info, rs) {
		return
	}
	if indexedSlice(info, rs) {
		return
	}
	for _, u := range ampNodes(info, rs.Body, v) {
		if escapesRange(info, rs, u) {
			pass.Reportf(u.Pos(), "this is the address of the per-iteration copy %s, not of the slice element: index the slice and take the address of the element", v.Name())
		}
	}
}

func rangeValue(info *types.Info, rs *ast.RangeStmt) (*types.Var, bool) {
	if rs.Value == nil {
		return nil, false
	}
	id, ok := ast.Unparen(rs.Value).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, false
	}
	return varOf(info, id)
}

func rangeStructElem(info *types.Info, rs *ast.RangeStmt) bool {
	t := info.TypeOf(rs.X)
	if t == nil {
		return false
	}
	var elem types.Type
	switch u := t.Underlying().(type) {
	case *types.Slice:
		elem = u.Elem()
	case *types.Array:
		elem = u.Elem()
	default:
		return false
	}
	_, ok := elem.Underlying().(*types.Struct)
	return ok
}

func indexedSlice(info *types.Info, rs *ast.RangeStmt) bool {
	target, ok := flow.KeyOf(info, rs.X)
	if !ok {
		return false
	}
	found := false
	ast.Inspect(rs.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.IndexExpr:
			found = keyHits(info, n.X, target)
		case *ast.IndexListExpr:
			found = keyHits(info, n.X, target)
		}
		return !found
	})
	return found
}

func keyHits(info *types.Info, e ast.Expr, target flow.Key) bool {
	k, ok := flow.KeyOf(info, e)
	return ok && k == target
}

func ampNodes(info *types.Info, body *ast.BlockStmt, v *types.Var) []*ast.UnaryExpr {
	var out []*ast.UnaryExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		if ampOf(info, u, v) {
			out = append(out, u)
		}
		return true
	})
	return out
}

func ampOf(info *types.Info, u *ast.UnaryExpr, v *types.Var) bool {
	k, ok := flow.KeyOf(info, u.X)
	return ok && k.Root == v
}

func escapesRange(info *types.Info, rs *ast.RangeStmt, u *ast.UnaryExpr) bool {
	if appendElemArg(info, rs.Body, u) {
		return true
	}
	if sendValue(rs.Body, u) {
		return true
	}
	if storedRangeAddr(info, rs, u) {
		return true
	}
	return returnedAddr(rs.Body, u)
}

func appendElemArg(info *types.Info, body *ast.BlockStmt, u *ast.UnaryExpr) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		found = elemArg(info, call, u)
		return !found
	})
	return found
}

func elemArg(info *types.Info, call *ast.CallExpr, u *ast.UnaryExpr) bool {
	if _, ok := isAppend(info, call); !ok {
		return false
	}
	for _, a := range call.Args[1:] {
		if ast.Unparen(a) == u {
			return true
		}
	}
	return false
}

func sendValue(body *ast.BlockStmt, u *ast.UnaryExpr) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.SendStmt)
		if !ok {
			return true
		}
		found = ast.Unparen(st.Value) == u
		return !found
	})
	return found
}

func storedRangeAddr(info *types.Info, rs *ast.RangeStmt, u *ast.UnaryExpr) bool {
	found := false
	ast.Inspect(rs.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		found = storesAddr(info, rs, st, u)
		return !found
	})
	return found
}

func storesAddr(info *types.Info, rs *ast.RangeStmt, st *ast.AssignStmt, u *ast.UnaryExpr) bool {
	for i, rhs := range st.Rhs {
		if ast.Unparen(rhs) != u {
			continue
		}
		if len(st.Lhs) == len(st.Rhs) {
			if storesOne(info, rs, st.Lhs[i]) {
				return true
			}
			continue
		}
		if storesTarget(info, rs, st.Lhs) {
			return true
		}
	}
	return false
}

func storesTarget(info *types.Info, rs *ast.RangeStmt, lhs []ast.Expr) bool {
	for _, l := range lhs {
		if storesOne(info, rs, l) {
			return true
		}
	}
	return false
}

func storesOne(info *types.Info, rs *ast.RangeStmt, l ast.Expr) bool {
	if isFieldOrIndex(info, l) {
		return true
	}
	if v, ok := identVar(info, l); ok && outsideRange(v, rs) {
		return true
	}
	return false
}

func isFieldOrIndex(info *types.Info, e ast.Expr) bool {
	switch n := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		return true
	case *ast.IndexExpr:
		return isMapIndex(info, n.X)
	case *ast.IndexListExpr:
		return isMapIndex(info, n.X)
	}
	return false
}

func isMapIndex(info *types.Info, x ast.Expr) bool {
	t := info.TypeOf(x)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

func outsideRange(v *types.Var, rs *ast.RangeStmt) bool {
	return v.Pos() < rs.Pos() || v.Pos() >= rs.End()
}

func returnedAddr(body *ast.BlockStmt, u *ast.UnaryExpr) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range st.Results {
			if ast.Unparen(r) == u {
				found = true
			}
		}
		return !found
	})
	return found
}
