package alias

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func runRangeref(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		findRanges(fn, func(rs *ast.RangeStmt) {
			checkRangeref(pass, fn, rs)
		})
	})
	return nil, nil
}

func checkRangeref(pass *analysis.Pass, fn ast.Node, rs *ast.RangeStmt) {
	info := pass.TypesInfo
	v, ok := rangeValue(info, rs)
	if !ok || !rangeStructElem(info, rs) {
		return
	}
	if indexedSlice(info, rs) {
		return
	}
	for _, u := range ampNodes(info, rs.Body, v) {
		if escapesRange(pass, fn, rs, u) {
			pass.Reportf(u.Pos(), "this is the address of the per-iteration copy %s, not of the slice element, so a write through it does not reach the slice element: index the slice and take the address of the element", v.Name())
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
	if passesPointer(info, u.X) {
		return false
	}
	k, ok := flow.KeyOf(info, u.X)
	return ok && k.Root == v
}

func escapesRange(pass *analysis.Pass, fn ast.Node, rs *ast.RangeStmt, u *ast.UnaryExpr) bool {
	info := pass.TypesInfo
	if appendElemArg(info, rs.Body, u) {
		return true
	}
	if sendValue(rs.Body, u) {
		return true
	}
	if storedMapAddr(info, rs, u) {
		return true
	}
	tk, store, ok := singleStoreTarget(info, rs, u)
	if !ok {
		return false
	}
	return writeThrough(pass, fn, store, tk, u)
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

func storedMapAddr(info *types.Info, rs *ast.RangeStmt, u *ast.UnaryExpr) bool {
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
		found = storesMap(info, st, u)
		return !found
	})
	return found
}

func storesMap(info *types.Info, st *ast.AssignStmt, u *ast.UnaryExpr) bool {
	for i, rhs := range st.Rhs {
		if ast.Unparen(rhs) != u {
			continue
		}
		if len(st.Lhs) == len(st.Rhs) {
			if isMapStore(info, st.Lhs[i]) {
				return true
			}
			continue
		}
		for _, l := range st.Lhs {
			if isMapStore(info, l) {
				return true
			}
		}
	}
	return false
}

func isMapStore(info *types.Info, l ast.Expr) bool {
	switch n := ast.Unparen(l).(type) {
	case *ast.IndexExpr:
		return isMapIndex(info, n.X)
	case *ast.IndexListExpr:
		return isMapIndex(info, n.X)
	}
	return false
}

func singleStoreTarget(info *types.Info, rs *ast.RangeStmt, u *ast.UnaryExpr) (flow.Key, *ast.AssignStmt, bool) {
	var tk flow.Key
	var store *ast.AssignStmt
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
		k, ok := singleStore(info, rs, st, u)
		if ok {
			tk = k
			store = st
			found = true
		}
		return !found
	})
	return tk, store, found
}

func singleStore(info *types.Info, rs *ast.RangeStmt, st *ast.AssignStmt, u *ast.UnaryExpr) (flow.Key, bool) {
	for i, rhs := range st.Rhs {
		if ast.Unparen(rhs) != u {
			continue
		}
		if len(st.Lhs) == len(st.Rhs) {
			if k, ok := singleTarget(info, rs, st.Lhs[i]); ok {
				return k, true
			}
			continue
		}
		for _, l := range st.Lhs {
			if k, ok := singleTarget(info, rs, l); ok {
				return k, true
			}
		}
	}
	return flow.Key{}, false
}

func singleTarget(info *types.Info, rs *ast.RangeStmt, l ast.Expr) (flow.Key, bool) {
	if _, ok := ast.Unparen(l).(*ast.SelectorExpr); ok {
		return flow.KeyOf(info, l)
	}
	if v, ok := identVar(info, l); ok && outsideRange(v, rs) {
		return flow.KeyOf(info, l)
	}
	return flow.Key{}, false
}

func writeThrough(pass *analysis.Pass, fn ast.Node, store *ast.AssignStmt, tk flow.Key, u *ast.UnaryExpr) bool {
	g := flow.NewGraph(pass, fn)
	if g == nil || store == nil {
		return false
	}
	info := pass.TypesInfo
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	whole := map[ast.Node]bool{}
	var writes []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		collectWholeWrite(info, st, tk, u, whole, &writes)
		return true
	})
	if len(writes) == 0 {
		return false
	}
	avoid := func(n ast.Node) bool {
		return whole[n]
	}
	for _, w := range writes {
		if g.Reaches(store, w, avoid) {
			return true
		}
	}
	return false
}

func collectWholeWrite(info *types.Info, st *ast.AssignStmt, tk flow.Key, u *ast.UnaryExpr, whole map[ast.Node]bool, writes *[]ast.Node) {
	hasWhole := false
	hasWrite := false
	for _, lhs := range st.Lhs {
		if lhs.Pos() <= u.End() {
			continue
		}
		if !hasWhole && isWholeTarget(info, lhs, tk) {
			hasWhole = true
		}
		if !hasWrite && matchesWrite(info, lhs, tk, u) {
			hasWrite = true
		}
	}
	if hasWhole {
		whole[st] = true
	}
	if hasWrite {
		*writes = append(*writes, st)
	}
}

func isWholeTarget(info *types.Info, lhs ast.Expr, tk flow.Key) bool {
	if _, ok := ast.Unparen(lhs).(*ast.StarExpr); ok {
		return false
	}
	wk, ok := flow.KeyOf(info, lhs)
	return ok && wk == tk
}

func matchesWrite(info *types.Info, lhs ast.Expr, tk flow.Key, u *ast.UnaryExpr) bool {
	if lhs.Pos() <= u.End() {
		return false
	}
	if _, ok := ast.Unparen(lhs).(*ast.StarExpr); ok {
		wk, ok := flow.KeyOf(info, lhs)
		return ok && wk.Root == tk.Root && pathMatches(tk.Path, wk.Path)
	}
	wk, ok := flow.KeyOf(info, lhs)
	if !ok || wk.Root != tk.Root {
		return false
	}
	if wk.Path == tk.Path {
		return false
	}
	return pathMatches(tk.Path, wk.Path)
}

func pathMatches(store, write string) bool {
	if store == "" {
		return true
	}
	if write == store {
		return true
	}
	return strings.HasPrefix(write, store+".")
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
