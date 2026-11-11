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

type outMemo struct {
	fn     inspector.Cursor
	idx    *funcIndex
	bare   map[*types.Var]bool
	valued map[*types.Var]bool
	takes  map[*types.Var][]token.Pos
}

func buildOutMemo(pass *analysis.Pass, fn inspector.Cursor) *outMemo {
	idx := buildFuncIndex(pass, fn)
	bare, valued := collectBare(pass, fn)
	takes := collectTakes(pass, fn)
	return &outMemo{fn: fn, idx: idx, bare: bare, valued: valued, takes: takes}
}

func collectBare(pass *analysis.Pass, fn inspector.Cursor) (bare, valued map[*types.Var]bool) {
	bare = map[*types.Var]bool{}
	valued = map[*types.Var]bool{}
	for cur := range fn.Preorder((*ast.ValueSpec)(nil)) {
		if owner(cur) != fn {
			continue
		}
		noteBare(pass, inspection.As[*ast.ValueSpec](cur.Node()), bare, valued)
	}
	return bare, valued
}

func noteBare(pass *analysis.Pass, vs *ast.ValueSpec, bare, valued map[*types.Var]bool) {
	for _, name := range vs.Names {
		v, ok := pass.TypesInfo.Defs[name].(*types.Var)
		if !ok {
			continue
		}
		if len(vs.Values) == 0 {
			bare[v] = true
		} else {
			valued[v] = true
		}
	}
}

func collectTakes(pass *analysis.Pass, fn inspector.Cursor) map[*types.Var][]token.Pos {
	takes := map[*types.Var][]token.Pos{}
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		noteTakes(pass, inspection.As[*ast.CallExpr](cur.Node()), takes)
	}
	return takes
}

func noteTakes(pass *analysis.Pass, call *ast.CallExpr, takes map[*types.Var][]token.Pos) {
	if v, ok := recvTaker(pass, call); ok {
		takes[v] = append(takes[v], call.Pos())
	}
	for idx, arg := range call.Args {
		if v, ok := argTaker(pass, call, idx, arg); ok {
			takes[v] = append(takes[v], call.Pos())
		}
	}
}

func recvTaker(pass *analysis.Pass, call *ast.CallExpr) (*types.Var, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	fn, ok := directMethod(pass, sel)
	if !ok {
		return nil, false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil, false
	}
	if _, isPtr := sig.Recv().Type().(*types.Pointer); !isPtr {
		return nil, false
	}
	return v, true
}

func argTaker(pass *analysis.Pass, call *ast.CallExpr, idx int, arg ast.Expr) (*types.Var, bool) {
	u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil, false
	}
	id, ok := ast.Unparen(u.X).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	if !paramTakesPointer(pass, call, idx) {
		return nil, false
	}
	return v, true
}

func checkOutSites(pass *analysis.Pass, fn inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	memo := buildOutMemo(pass, fn)
	for cur := range fn.Preorder((*ast.ExprStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		checkOneOutSite(pass, cur, memo, f, hash, reported)
	}
}

func checkOneOutSite(pass *analysis.Pass, cur inspector.Cursor, memo *outMemo, f facts, hash *types.Interface, reported map[token.Pos]bool) {
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
	if isExempt(pass, call, f, hash) {
		return
	}
	xobj, xname := outCandidate(pass, call, memo)
	if xobj == nil {
		return
	}
	callee := types.ExprString(call.Fun)
	pass.Reportf(call.Pos(), "%s is read after the error of %s is dropped, and it may hold partial results when the call fails: check the error, or write _ = %s(...) to state that partial results are intended", xname, callee, callee)
	reported[call.Pos()] = true
}

func dropsError(info *types.Info, call *ast.CallExpr) bool {
	return slices.ContainsFunc(resultTypes(info, call), isError)
}

func outCandidate(pass *analysis.Pass, call *ast.CallExpr, memo *outMemo) (found *types.Var, name string) {
	for idx, arg := range call.Args {
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
		if !decodeTarget(pass, call, idx) {
			continue
		}
		if outHolds(call, v, memo) {
			return v, id.Name
		}
	}
	return nil, ""
}

func decodeTarget(pass *analysis.Pass, call *ast.CallExpr, idx int) bool {
	if isVariadicAny(pass, call, idx) {
		return isScanOut(pass, call)
	}
	pt, ok := callParamType(pass, call, idx)
	if !ok {
		return false
	}
	if isEmptyInterface(pt) {
		return true
	}
	if isPointerType(pt) {
		return true
	}
	return isWriterInterface(pt)
}

func isVariadicAny(pass *analysis.Pass, call *ast.CallExpr, idx int) bool {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params() == nil || !sig.Variadic() {
		return false
	}
	n := sig.Params().Len()
	if idx < n-1 {
		return false
	}
	slice, ok := types.Unalias(sig.Params().At(n - 1).Type()).(*types.Slice)
	if !ok {
		return false
	}
	return isEmptyInterface(slice.Elem())
}

func isScanOut(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn == nil || fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() != "fmt" {
		return false
	}
	switch fn.Name() {
	case "Scan", "Scanf", "Scanln", "Sscan", "Sscanf", "Sscanln", "Fscan", "Fscanf", "Fscanln":
		return true
	}
	return false
}

func isEmptyInterface(t types.Type) bool {
	iface, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok && iface.NumMethods() == 0
}

func isPointerType(t types.Type) bool {
	_, ok := types.Unalias(t).(*types.Pointer)
	return ok
}

func callParamType(pass *analysis.Pass, call *ast.CallExpr, idx int) (types.Type, bool) {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return nil, false
	}
	sig, ok := fnobj.Type().(*types.Signature)
	if !ok || sig.Params() == nil {
		return nil, false
	}
	return paramTypeAt(sig, idx, call)
}

func outHolds(call *ast.CallExpr, x *types.Var, memo *outMemo) bool {
	if !declaredHere(memo.fn, x) {
		return false
	}
	if !isBare(memo, x) {
		return false
	}
	if len(memo.idx.assigns[x]) > 0 {
		return false
	}
	if memo.idx.hasPathWriteBefore(x, call.Pos()) {
		return false
	}
	if takenBefore(memo, x, call.Pos()) {
		return false
	}
	_, ok := memo.idx.firstRead(x, call.End())
	return ok
}

func isBare(memo *outMemo, x *types.Var) bool {
	return memo.bare[x] && !memo.valued[x]
}

func takenBefore(memo *outMemo, x *types.Var, before token.Pos) bool {
	list := memo.takes[x]
	return len(list) > 0 && list[0] < before
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
