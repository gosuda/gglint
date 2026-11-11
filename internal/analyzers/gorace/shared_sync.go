package gorace

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func isChanType(t types.Type) bool {
	if t == nil {
		return false
	}
	u := types.Unalias(t)
	if p, ok := u.(*types.Pointer); ok {
		u = types.Unalias(p.Elem())
	}
	_, ok := u.Underlying().(*types.Chan)
	return ok
}

func isSyncPrimitive(t types.Type) bool {
	if t == nil {
		return false
	}
	if isChanType(t) {
		return true
	}
	u := types.Unalias(t)
	if p, ok := u.(*types.Pointer); ok {
		u = types.Unalias(p.Elem())
	}
	n, ok := u.(*types.Named)
	if !ok || n.Obj() == nil || n.Obj().Pkg() == nil {
		return false
	}
	if n.Obj().Pkg().Path() != syncPkgPath {
		return false
	}
	switch n.Obj().Name() {
	case "Mutex", "RWMutex", "WaitGroup", "Cond", "Once":
		return true
	}
	return false
}

func structHasSync(t types.Type) bool {
	if t == nil {
		return false
	}
	u := types.Unalias(t)
	if p, ok := u.(*types.Pointer); ok {
		u = types.Unalias(p.Elem())
	}
	st, ok := u.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for field := range st.Fields() {
		if isSyncPrimitive(field.Type()) {
			return true
		}
	}
	return false
}

func syncKeyType(_ *types.Info, k flow.Key) types.Type {
	root, ok := k.Root.(*types.Var)
	if !ok || root == nil {
		return nil
	}
	t := root.Type()
	if t == nil {
		return nil
	}
	path := k.Path
	if path != "" && path[0] == '.' {
		path = path[1:]
	}
	for path != "" {
		name, rest := splitPath(path)
		next, ok := lookupStructField(t, name)
		if !ok {
			return nil
		}
		t = next
		path = rest
	}
	return t
}

func splitPath(path string) (name, rest string) {
	if j := indexDot(path); j >= 0 {
		return path[:j], path[j+1:]
	}
	return path, ""
}

func lookupStructField(t types.Type, name string) (types.Type, bool) {
	u := types.Unalias(t)
	if p, ok := u.(*types.Pointer); ok {
		u = types.Unalias(p.Elem())
	}
	st, ok := u.Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}
	for field := range st.Fields() {
		if field.Name() == name {
			return field.Type(), true
		}
	}
	return nil, false
}

func indexDot(s string) int {
	for i := range len(s) {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

func isSyncObject(info *types.Info, k flow.Key) bool {
	t := syncKeyType(info, k)
	if t == nil {
		return false
	}
	return isSyncPrimitive(t) || structHasSync(t)
}

func spawnedLits(pass *analysis.Pass, fn ast.Node) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	for _, s := range flow.Spawns(pass, fn) {
		if s.Body != nil {
			out[s.Body] = true
		}
	}
	return out
}

func boundLitSync(pass *analysis.Pass, fn ast.Node, call *ast.CallExpr) *ast.FuncLit {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return nil
	}
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out *ast.FuncLit
	ast.Inspect(body, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok && n != ast.Node(body) {
			return false
		}
		switch t := n.(type) {
		case *ast.AssignStmt:
			out = assignBoundLit(pass, t, v)
		case *ast.ValueSpec:
			out = valueBoundLit(pass, t, v)
		}
		return out == nil
	})
	return out
}

func assignBoundLit(pass *analysis.Pass, a *ast.AssignStmt, v *types.Var) *ast.FuncLit {
	if len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return nil
	}
	lid, ok := ast.Unparen(a.Lhs[0]).(*ast.Ident)
	if !ok || identVarSync(pass.TypesInfo, lid) != v {
		return nil
	}
	lit, ok := ast.Unparen(a.Rhs[0]).(*ast.FuncLit)
	if !ok {
		return nil
	}
	return lit
}

func valueBoundLit(pass *analysis.Pass, spec *ast.ValueSpec, v *types.Var) *ast.FuncLit {
	if len(spec.Names) != 1 || len(spec.Values) != 1 {
		return nil
	}
	if identVarSync(pass.TypesInfo, spec.Names[0]) != v {
		return nil
	}
	lit, ok := ast.Unparen(spec.Values[0]).(*ast.FuncLit)
	if !ok {
		return nil
	}
	return lit
}

func identVarSync(info *types.Info, id *ast.Ident) *types.Var {
	if v, ok := info.Defs[id].(*types.Var); ok {
		return v
	}
	if v, ok := info.Uses[id].(*types.Var); ok {
		return v
	}
	return nil
}

func receiverAlias(pass *analysis.Pass, fn ast.Node, v *types.Var) *types.Var {
	body := flow.Body(fn)
	if body == nil || v == nil {
		return v
	}
	var out *types.Var
	ast.Inspect(body, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok && n != ast.Node(body) {
			return false
		}
		switch t := n.(type) {
		case *ast.ValueSpec:
			out = valueReceiverAlias(pass, t, v)
		case *ast.AssignStmt:
			out = assignReceiverAlias(pass, t, v)
		}
		return out == nil
	})
	if out != nil {
		return out
	}
	return v
}

func valueReceiverAlias(pass *analysis.Pass, spec *ast.ValueSpec, v *types.Var) *types.Var {
	for i, name := range spec.Names {
		if identVarSync(pass.TypesInfo, name) != v {
			continue
		}
		if len(spec.Values) != len(spec.Names) && len(spec.Values) != 1 {
			continue
		}
		rhs := spec.Values[0]
		if len(spec.Values) == len(spec.Names) {
			rhs = spec.Values[i]
		}
		if id, ok := ast.Unparen(rhs).(*ast.Ident); ok {
			if u := identVarSync(pass.TypesInfo, id); u != nil && u != v {
				return u
			}
		}
	}
	return nil
}

func assignReceiverAlias(pass *analysis.Pass, a *ast.AssignStmt, v *types.Var) *types.Var {
	if len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return nil
	}
	lid, ok := ast.Unparen(a.Lhs[0]).(*ast.Ident)
	if !ok || identVarSync(pass.TypesInfo, lid) != v {
		return nil
	}
	if rid, ok := ast.Unparen(a.Rhs[0]).(*ast.Ident); ok {
		if u := identVarSync(pass.TypesInfo, rid); u != nil && u != v {
			return u
		}
	}
	return nil
}
