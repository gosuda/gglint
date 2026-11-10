package errdrop

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const closeMethodName = "Close"

func checkDurableSites(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) {
	for cur := range fn.Preorder((*ast.ExprStmt)(nil), (*ast.DeferStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		checkOneDurableSite(pass, fn, cur, set, hash, reported)
	}
}

func checkOneDurableSite(pass *analysis.Pass, fn, cur inspector.Cursor, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) {
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
	if isExempt(pass, call, set, hash) {
		return
	}
	if !durableHolds(pass, fn, xobj) {
		return
	}
	pass.Reportf(call.Pos(), "%s is written in this function and the error of %s.Close is dropped, so a failed Close loses data: return or check the Close error", xname, xname)
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
	return assignedFromCreate(pass, fn, x) || writtenByMethod(pass, fn, x) || writtenByFirstArg(pass, fn, x) || rdwrWithEvidence(pass, fn, x)
}

func rdwrWithEvidence(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	if !assignedFromRdwrOnly(pass, fn, x) {
		return false
	}
	return passedAsArg(pass, fn, x)
}

func assignedFromRdwrOnly(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if isRdwrOpenAssign(pass, inspection.As[*ast.AssignStmt](cur.Node()), x) {
			return true
		}
	}
	return false
}

func isRdwrOpenAssign(pass *analysis.Pass, as *ast.AssignStmt, x *types.Var) bool {
	if !assignTargetsVar(pass.TypesInfo, as, x) || len(as.Rhs) != 1 {
		return false
	}
	call := callOf(as.Rhs[0])
	if call == nil {
		return false
	}
	return isRdwrOpenCall(pass, call)
}

func isRdwrOpenCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil || fnobj.FullName() != "os.OpenFile" {
		return false
	}
	return len(call.Args) >= 2 && flagRdwrOnly(pass.TypesInfo, call.Args[1])
}

func flagRdwrOnly(info *types.Info, e ast.Expr) bool {
	v := info.Types[ast.Unparen(e)].Value
	if v == nil {
		return false
	}
	n, ok := constant.Int64Val(v)
	if !ok {
		return false
	}
	if n&int64(os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return false
	}
	return n&int64(os.O_RDWR) != 0
}

func passedAsArg(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := inspection.As[*ast.CallExpr](cur.Node())
		for _, arg := range call.Args {
			id, ok := ast.Unparen(arg).(*ast.Ident)
			if !ok {
				continue
			}
			if pass.TypesInfo.ObjectOf(id) == types.Object(x) {
				return true
			}
		}
	}
	return false
}

func assignedFromCreate(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if createWritesFile(pass, inspection.As[*ast.AssignStmt](cur.Node()), x) {
			return true
		}
	}
	return false
}

func createWritesFile(pass *analysis.Pass, as *ast.AssignStmt, x *types.Var) bool {
	if !assignTargetsVar(pass.TypesInfo, as, x) {
		return false
	}
	if len(as.Rhs) != 1 {
		return false
	}
	call := callOf(as.Rhs[0])
	if call == nil {
		return false
	}
	return createCallWritable(pass, call)
}

func assignTargetsVar(info *types.Info, as *ast.AssignStmt, x *types.Var) bool {
	for _, lhs := range as.Lhs {
		if resolvesTo(info, lhs, []*types.Var{x}) {
			return true
		}
	}
	return false
}

func createCallWritable(pass *analysis.Pass, call *ast.CallExpr) bool {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return false
	}
	switch fnobj.FullName() {
	case "os.Create", "os.CreateTemp":
		return true
	case "os.OpenFile":
		return len(call.Args) >= 2 && flagWritable(pass.TypesInfo, call.Args[1])
	}
	return false
}

func flagWritable(info *types.Info, e ast.Expr) bool {
	v := info.Types[ast.Unparen(e)].Value
	if v == nil {
		return false
	}
	n, ok := constant.Int64Val(v)
	return ok && n&int64(os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
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
		if isWriteMethodName(sel.Sel.Name) {
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

func writtenByFirstArg(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if firstArgTakesWriter(pass, inspection.As[*ast.CallExpr](cur.Node()), x) {
			return true
		}
	}
	return false
}

func firstArgTakesWriter(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	if len(call.Args) == 0 {
		return false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return false
	}
	sig, ok := fnobj.Type().(*types.Signature)
	if !ok || sig.Params() == nil || sig.Params().Len() == 0 {
		return false
	}
	return isWriterInterface(sig.Params().At(0).Type())
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
