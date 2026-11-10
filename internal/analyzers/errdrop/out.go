package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

func checkOutSites(pass *analysis.Pass, fn inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	for cur := range fn.Preorder((*ast.ExprStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		checkOneOutSite(pass, fn, cur, f, hash, reported)
	}
}

func checkOneOutSite(pass *analysis.Pass, fn, cur inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	es := inspection.As[*ast.ExprStmt](cur.Node())
	call := callOf(es.X)
	if call == nil || !isRealCall(pass.TypesInfo, call) {
		return
	}
	if reported[call.Pos()] {
		return
	}
	if !dropsError(pass.TypesInfo, call) {
		return
	}
	if isExempt(pass, call, f.never, hash) {
		return
	}
	xobj, xname := outCandidate(pass, fn, call)
	if xobj == nil {
		return
	}
	if outSinkExempt(pass, call, xobj, f.never) {
		return
	}
	callee := types.ExprString(call.Fun)
	pass.Reportf(call.Pos(), "%s is read after the error of %s is dropped, and it may hold partial results when the call fails: check the error, or write _ = %s(...) to state that partial results are intended", xname, callee, callee)
	reported[call.Pos()] = true
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
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := inspection.As[*ast.CallExpr](cur.Node())
		if call.Pos() >= before {
			continue
		}
		if callTakesPointer(pass, call, x) {
			return true
		}
	}
	return false
}

func callTakesPointer(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	if methodTakesRecv(pass, call, x) {
		return true
	}
	for i, arg := range call.Args {
		if takesPointerAt(pass, call, i, arg, x) {
			return true
		}
	}
	return false
}

func methodTakesRecv(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	fn, ok := directMethod(pass, sel)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	_, isPtr := sig.Recv().Type().(*types.Pointer)
	return isPtr
}

func takesPointerAt(pass *analysis.Pass, call *ast.CallExpr, idx int, arg ast.Expr, x *types.Var) bool {
	u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return false
	}
	id, ok := ast.Unparen(u.X).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	return paramTakesPointer(pass, call, idx)
}

func paramTakesPointer(pass *analysis.Pass, call *ast.CallExpr, idx int) bool {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return false
	}
	sig, ok := fnobj.Type().(*types.Signature)
	if !ok || sig.Params() == nil {
		return false
	}
	pt, ok := paramTypeAt(sig, idx, call)
	if !ok {
		return false
	}
	if sig.Variadic() && idx >= sig.Params().Len()-1 {
		if _, isIface := types.Unalias(pt).Underlying().(*types.Interface); isIface {
			return false
		}
	}
	return true
}

func paramTypeAt(sig *types.Signature, idx int, call *ast.CallExpr) (types.Type, bool) {
	n := sig.Params().Len()
	if n == 0 {
		return nil, false
	}
	if !sig.Variadic() {
		if idx >= n {
			return nil, false
		}
		return sig.Params().At(idx).Type(), true
	}
	if idx < n-1 {
		return sig.Params().At(idx).Type(), true
	}
	if call.Ellipsis.IsValid() {
		if idx != n-1 {
			return nil, false
		}
		return sig.Params().At(n - 1).Type(), true
	}
	slice, ok := types.Unalias(sig.Params().At(n - 1).Type()).(*types.Slice)
	if !ok {
		return nil, false
	}
	return slice.Elem(), true
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

func outSinkExempt(pass *analysis.Pass, call *ast.CallExpr, x *types.Var, set map[*types.Func]bool) bool {
	for idx, arg := range call.Args {
		if sinkArgExempt(pass, idx, call, arg, x, set) {
			return true
		}
	}
	return false
}

func sinkArgExempt(pass *analysis.Pass, idx int, call *ast.CallExpr, arg ast.Expr, x *types.Var, set map[*types.Func]bool) bool {
	if !isAddrOf(pass, arg, x) {
		return false
	}
	sig, ok := sinkSignatureFor(pass, call)
	if !ok {
		return false
	}
	pt, ok := paramTypeAt(sig, idx, call)
	if !ok || !isInterfaceType(pt) {
		return false
	}
	dst := sinkDestType(pass, arg, x)
	if sinkWantsWrite(pt) {
		if writeHasFact(pass, dst, set) {
			return true
		}
	}
	if sinkWantsWriteString(pt) {
		if writeStringHasFact(pass, dst, set) {
			return true
		}
	}
	return false
}

func sinkWantsWrite(pt types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(pt, false, nil, "Write")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return writerSigShape(sig)
}

func sinkWantsWriteString(pt types.Type) bool {
	sig := writeStringSig(pt)
	if sig == nil {
		return false
	}
	return writeStringParams(sig) && writeStringResults(sig)
}

func writeStringSig(pt types.Type) *types.Signature {
	obj, _, _ := types.LookupFieldOrMethod(pt, false, nil, "WriteString")
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	return sig
}

func writeStringParams(sig *types.Signature) bool {
	if sig.Params() == nil || sig.Params().Len() != 1 {
		return false
	}
	s, ok := types.Unalias(sig.Params().At(0).Type()).(*types.Basic)
	return ok && s.Kind() == types.String
}

func writeStringResults(sig *types.Signature) bool {
	if sig.Results() == nil || sig.Results().Len() != 2 {
		return false
	}
	return types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int]) && isError(sig.Results().At(1).Type())
}

func sinkSignatureFor(pass *analysis.Pass, call *ast.CallExpr) (*types.Signature, bool) {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return nil, false
	}
	sig, ok := fnobj.Type().(*types.Signature)
	return sig, ok
}

func isAddrOf(pass *analysis.Pass, arg ast.Expr, x *types.Var) bool {
	u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return false
	}
	id, ok := ast.Unparen(u.X).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(x)
}

func isInterfaceType(t types.Type) bool {
	_, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok
}

func sinkDestType(pass *analysis.Pass, arg ast.Expr, x *types.Var) types.Type {
	if dst := pass.TypesInfo.TypeOf(arg); dst != nil {
		return dst
	}
	return x.Type()
}
