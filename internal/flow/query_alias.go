package flow

import (
	"go/ast"
	"go/types"
)

func collectAliases(info *types.Info, body *ast.BlockStmt, v *types.Var) map[*types.Var]bool {
	out := map[*types.Var]bool{v: true}
	if !isRef(v.Type()) {
		return out
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				changed = aliasAssign(info, n, out) || changed
			case *ast.ValueSpec:
				changed = aliasSpec(info, n, out) || changed
			}
			return true
		})
	}
	return out
}

func aliasAssign(info *types.Info, s *ast.AssignStmt, out map[*types.Var]bool) bool {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return false
	}
	if !resolvesTo(info, s.Rhs[0], out) {
		return false
	}
	return aliasLHS(info, s.Lhs[0], out)
}

func aliasSpec(info *types.Info, s *ast.ValueSpec, out map[*types.Var]bool) bool {
	if len(s.Names) != 1 || len(s.Values) != 1 {
		return false
	}
	if !resolvesTo(info, s.Values[0], out) {
		return false
	}
	if vv, ok := info.Defs[s.Names[0]].(*types.Var); ok && !out[vv] {
		out[vv] = true
		return true
	}
	return false
}

func aliasLHS(info *types.Info, lhs ast.Expr, out map[*types.Var]bool) bool {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return false
	}
	if vv, ok := info.Defs[id].(*types.Var); ok && !out[vv] {
		out[vv] = true
		return true
	}
	if vv, ok := info.Uses[id].(*types.Var); ok && !out[vv] {
		out[vv] = true
		return true
	}
	return false
}

func resolvesTo(info *types.Info, e ast.Expr, out map[*types.Var]bool) bool {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		return resolvesIdent(info, id, out)
	}
	if sel, ok := e.(*ast.SelectorExpr); ok && info.Selections[sel] != nil && info.Selections[sel].Kind() == types.MethodVal {
		return resolvesTo(info, sel.X, out)
	}
	if s, ok := e.(*ast.SliceExpr); ok {
		return resolvesTo(info, s.X, out)
	}
	if u, ok := e.(*ast.TypeAssertExpr); ok {
		return resolvesTo(info, u.X, out)
	}
	if c, ok := e.(*ast.CallExpr); ok {
		return resolvesCall(info, c, out)
	}
	if c, ok := e.(*ast.CompositeLit); ok {
		return resolvesComposite(info, c, out)
	}
	return false
}

func resolvesIdent(info *types.Info, id *ast.Ident, out map[*types.Var]bool) bool {
	if vv, ok := info.Uses[id].(*types.Var); ok && out[vv] {
		return true
	}
	if vv, ok := info.Defs[id].(*types.Var); ok && out[vv] {
		return true
	}
	return false
}

func resolvesCall(info *types.Info, c *ast.CallExpr, out map[*types.Var]bool) bool {
	if !info.Types[c.Fun].IsType() || len(c.Args) != 1 {
		return false
	}
	if !canCarry(info.TypeOf(c)) {
		return false
	}
	if !canCarry(info.TypeOf(c.Args[0])) {
		return false
	}
	return resolvesTo(info, c.Args[0], out)
}

func resolvesComposite(info *types.Info, c *ast.CompositeLit, out map[*types.Var]bool) bool {
	for _, elt := range c.Elts {
		if resolvesElt(info, elt, out) {
			return true
		}
	}
	return false
}

func resolvesElt(info *types.Info, elt ast.Expr, out map[*types.Var]bool) bool {
	if kv, ok := elt.(*ast.KeyValueExpr); ok {
		return resolvesTo(info, kv.Value, out)
	}
	return resolvesTo(info, elt, out)
}

func canCarry(t types.Type) bool {
	return carryRef(t, map[types.Type]bool{})
}

func carryRef(t types.Type, seen map[types.Type]bool) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		if seen[named] {
			return false
		}
		seen[named] = true
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Slice, *types.Signature, *types.Chan, *types.Interface:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Array:
		return carryRef(u.Elem(), seen)
	case *types.Struct:
		return structCarry(u, seen)
	default:
		return false
	}
}

func structCarry(u *types.Struct, seen map[types.Type]bool) bool {
	for f := range u.Fields() {
		if carryRef(f.Type(), seen) {
			return true
		}
	}
	return false
}

func isRef(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Slice, *types.Signature, *types.Chan, *types.Interface:
		return true
	}
	if b, ok := t.Underlying().(*types.Basic); ok && b.Kind() == types.UnsafePointer {
		return true
	}
	return false
}
