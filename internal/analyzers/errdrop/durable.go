package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const closeMethodName = "Close"

func checkDurableSites(pass *analysis.Pass, fn inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	for cur := range fn.Preorder((*ast.ExprStmt)(nil), (*ast.DeferStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		checkOneDurableSite(pass, fn, cur, f, hash, reported)
	}
}

func checkOneDurableSite(pass *analysis.Pass, fn, cur inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	call := durableCall(cur)
	if call == nil || !isRealCall(pass.TypesInfo, call) {
		return
	}
	if reported[call.Pos()] {
		return
	}
	xobj, xname := closeReceiver(pass, call)
	if xobj == nil {
		return
	}
	if !isExactlyError(pass.TypesInfo, call) {
		return
	}
	if isExempt(pass, call, f.never, hash) {
		return
	}
	if !durableHolds(pass, fn, xobj) {
		return
	}
	pass.Reportf(call.Pos(), "%s is opened for writing in this function and the error of %s.Close is dropped, so a failed Close can lose written data: return or check the Close error", xname, xname)
	reported[call.Pos()] = true
}

func durableCall(cur inspector.Cursor) *ast.CallExpr {
	switch n := cur.Node().(type) {
	case *ast.ExprStmt:
		return callOf(n.X)
	case *ast.DeferStmt:
		if call := callOf(n.Call); call != nil {
			return call
		}
		return n.Call
	}
	return nil
}

func closeReceiver(pass *analysis.Pass, call *ast.CallExpr) (found *types.Var, name string) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != closeMethodName {
		return nil, ""
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, ""
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return v, id.Name
	}
	return nil, ""
}

func isExactlyError(info *types.Info, call *ast.CallExpr) bool {
	t := info.TypeOf(call)
	return t != nil && isError(t)
}

func durableHolds(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	if !isLocalPtrStruct(fn, x) {
		return false
	}
	if isConnection(x.Type()) {
		return false
	}
	if !writtenInFunc(pass, fn, x) {
		return false
	}
	return !hasUsedCloseSync(pass, fn, x)
}

func isLocalPtrStruct(fn inspector.Cursor, x *types.Var) bool {
	if !declaredHere(fn, x) {
		return false
	}
	ptr, ok := types.Unalias(x.Type()).(*types.Pointer)
	if !ok {
		return false
	}
	_, ok = types.Unalias(ptr.Elem()).Underlying().(*types.Struct)
	return ok
}

func writtenInFunc(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	return writtenByMethod(pass, fn, x) || writtenByAnyArgWriter(pass, fn, x) || assignedToWriterInterface(pass, fn, x)
}

func writtenByMethod(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := inspection.As[*ast.CallExpr](cur.Node())
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.ObjectOf(id) != types.Object(x) {
			continue
		}
		if len(call.Args) > 0 && isWriteMethodName(sel.Sel.Name) {
			return true
		}
	}
	return false
}

func isWriteMethodName(name string) bool {
	if name == "ReadFrom" {
		return true
	}
	return name != "WriteTo" && strings.HasPrefix(name, "Write")
}

func isWriterInterface(t types.Type) bool {
	if _, ok := types.Unalias(t).Underlying().(*types.Interface); !ok {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(t, false, nil, "Write")
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

func writerSigShape(sig *types.Signature) bool {
	if sig == nil {
		return false
	}
	if sig.Params() == nil || sig.Params().Len() != 1 {
		return false
	}
	if sig.Results() == nil || sig.Results().Len() != 2 {
		return false
	}
	return writerParamIsBytes(sig.Params().At(0).Type()) && writerResultsShape(sig)
}

func writerParamIsBytes(p0 types.Type) bool {
	slice, ok := types.Unalias(p0).(*types.Slice)
	return ok && types.Identical(slice.Elem(), types.Typ[types.Uint8])
}

func writerResultsShape(sig *types.Signature) bool {
	return types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int]) && isError(sig.Results().At(1).Type())
}

func hasUsedCloseSync(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := inspection.As[*ast.CallExpr](cur.Node())
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != closeMethodName && sel.Sel.Name != "Sync") {
			continue
		}
		id, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
			continue
		}
		if resultUsed(cur) {
			return true
		}
	}
	return false
}

func resultUsed(cur inspector.Cursor) bool {
	p := skipParens(cur.Parent())
	switch n := p.Node().(type) {
	case *ast.ExprStmt, *ast.DeferStmt, *ast.GoStmt:
		return false
	case *ast.AssignStmt:
		return !allBlank(n.Lhs)
	}
	return true
}

func writtenByAnyArgWriter(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if callWritesViaWriter(pass, inspection.As[*ast.CallExpr](cur.Node()), x) {
			return true
		}
	}
	return false
}

func callWritesViaWriter(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return false
	}
	sig, ok := fnobj.Type().(*types.Signature)
	if !ok {
		return false
	}
	for idx, arg := range call.Args {
		if argIsWriter(pass, sig, idx, call, arg, x) {
			return true
		}
	}
	return false
}

func argIsWriter(pass *analysis.Pass, sig *types.Signature, idx int, call *ast.CallExpr, arg ast.Expr, x *types.Var) bool {
	id, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	pt, ok := paramTypeAt(sig, idx, call)
	return ok && isWriterInterface(pt)
}

func assignedToWriterInterface(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	return assignStmtWritesToWriter(pass, fn, x) || valueSpecWritesToWriter(pass, fn, x)
}

func assignStmtWritesToWriter(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		as := inspection.As[*ast.AssignStmt](cur.Node())
		if writesToWriter(pass, as, x) {
			return true
		}
	}
	return false
}

func writesToWriter(pass *analysis.Pass, as *ast.AssignStmt, x *types.Var) bool {
	if len(as.Lhs) != len(as.Rhs) && len(as.Rhs) != 1 {
		return false
	}
	for i, lhs := range as.Lhs {
		if pairWritesToWriter(pass, as, i, lhs, x) {
			return true
		}
	}
	return false
}

func pairWritesToWriter(pass *analysis.Pass, as *ast.AssignStmt, i int, lhs ast.Expr, x *types.Var) bool {
	rhs := as.Rhs[0]
	if len(as.Rhs) == len(as.Lhs) {
		rhs = as.Rhs[i]
	}
	if id, ok := ast.Unparen(rhs).(*ast.Ident); !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	return pass.TypesInfo.TypeOf(lhs) != nil && isWriterInterface(pass.TypesInfo.TypeOf(lhs))
}

func valueSpecWritesToWriter(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.ValueSpec)(nil)) {
		if owner(cur) != fn {
			continue
		}
		vs := inspection.As[*ast.ValueSpec](cur.Node())
		if specWritesToWriter(pass, vs, x) {
			return true
		}
	}
	return false
}

func specWritesToWriter(pass *analysis.Pass, vs *ast.ValueSpec, x *types.Var) bool {
	if len(vs.Values) == 0 {
		return false
	}
	for i, name := range vs.Names {
		v, ok := valueForName(vs, i)
		if !ok {
			continue
		}
		if specPairWrites(pass, v, name, x) {
			return true
		}
	}
	return false
}

func valueForName(vs *ast.ValueSpec, i int) (ast.Expr, bool) {
	switch {
	case len(vs.Values) == len(vs.Names):
		return vs.Values[i], true
	case len(vs.Values) == 1:
		return vs.Values[0], true
	default:
		return nil, false
	}
}

func specPairWrites(pass *analysis.Pass, v ast.Expr, name *ast.Ident, x *types.Var) bool {
	id, ok := ast.Unparen(v).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	obj, ok := pass.TypesInfo.Defs[name].(*types.Var)
	return ok && isWriterInterface(obj.Type())
}

func isConnection(t types.Type) bool {
	if !hasSetDeadline(t) {
		return false
	}
	return hasRemoteAddr(t)
}

func hasSetDeadline(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "SetDeadline")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params() == nil || sig.Params().Len() != 1 || sig.Results() == nil || sig.Results().Len() != 1 {
		return false
	}
	if !isError(sig.Results().At(0).Type()) {
		return false
	}
	return isTimeType(sig.Params().At(0).Type())
}

func hasRemoteAddr(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "RemoteAddr")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	if sig.Params() != nil && sig.Params().Len() != 0 {
		return false
	}
	if sig.Results() == nil || sig.Results().Len() != 1 {
		return false
	}
	return isNetAddrType(sig.Results().At(0).Type())
}

func isTimeType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time"
}

func isNetAddrType(t types.Type) bool {
	if _, ok := types.Unalias(t).Underlying().(*types.Interface); !ok {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "net" && named.Obj().Name() == "Addr"
}
