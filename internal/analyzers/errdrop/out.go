package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

func checkOutSites(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) {
	for cur := range fn.Preorder((*ast.ExprStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		es := inspection.As[*ast.ExprStmt](cur.Node())
		call := callOf(es.X)
		if call == nil || !isRealCall(pass.TypesInfo, call) {
			continue
		}
		if reported[call.Pos()] {
			continue
		}
		if !dropsError(pass.TypesInfo, call) {
			continue
		}
		if isExempt(pass, call, set, hash) {
			continue
		}
		xobj, xname := outCandidate(pass, fn, call)
		if xobj == nil {
			continue
		}
		callee := types.ExprString(call.Fun)
		pass.Reportf(call.Pos(), "%s is read after the error of %s is dropped, and it keeps its zero value when the call fails: check the error, or write _ = %s(...) to state that the zero value is intended", xname, callee, callee)
		reported[call.Pos()] = true
	}
}

func dropsError(info *types.Info, call *ast.CallExpr) bool {
	return slices.ContainsFunc(resultTypes(info, call), isError)
}

func outCandidate(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr) (found *types.Var, name string) {
	for _, arg := range call.Args {
		u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			continue
		}
		id, ok := ast.Unparen(u.X).(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		if outHolds(pass, fn, call, v) {
			return v, id.Name
		}
	}
	return nil, ""
}

func outHolds(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr, x *types.Var) bool {
	if !declaredHere(fn, x) {
		return false
	}
	if !isBareVarDecl(pass, fn, x) {
		return false
	}
	if assignsVar(pass, fn, x) {
		return false
	}
	if takenBefore(pass, fn, x, call.Pos()) {
		return false
	}
	return mentionedAfter(pass, fn, x, call.End())
}

func takenBefore(pass *analysis.Pass, fn inspector.Cursor, x *types.Var, before token.Pos) bool {
	for cur := range fn.Preorder((*ast.UnaryExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		u := inspection.As[*ast.UnaryExpr](cur.Node())
		if u.Op != token.AND || u.Pos() >= before {
			continue
		}
		id, ok := ast.Unparen(u.X).(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.ObjectOf(id) == types.Object(x) {
			return true
		}
	}
	return false
}

func isBareVarDecl(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	found := false
	none := true
	for cur := range fn.Preorder((*ast.ValueSpec)(nil)) {
		if owner(cur) != fn {
			continue
		}
		vs := inspection.As[*ast.ValueSpec](cur.Node())
		for _, name := range vs.Names {
			if pass.TypesInfo.Defs[name] == x {
				found = true
				if len(vs.Values) != 0 {
					none = false
				}
			}
		}
	}
	return found && none
}

func assignsVar(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if nodeAssignsVar(pass.TypesInfo, cur.Node(), x) {
			return true
		}
	}
	return false
}

func mentionedAfter(pass *analysis.Pass, fn inspector.Cursor, x *types.Var, after token.Pos) bool {
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if id.Pos() <= after {
			continue
		}
		if pass.TypesInfo.Uses[id] == x {
			return true
		}
	}
	return false
}
