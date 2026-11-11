package errdrop

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const closeMethodName = "Close"

type durableMemo struct {
	fn          inspector.Cursor
	method      map[*types.Var][]token.Pos
	arg         map[*types.Var][]token.Pos
	opened      map[*types.Var]bool
	storedVar   map[*types.Var][]*types.Var
	storedField map[*types.Var][]fieldStore
	called      map[*types.Var][]pathUse
	passed      map[*types.Var][]pathUse
	used        map[*types.Var]bool
}

type fieldStore struct {
	root *types.Var
	path string
	pos  token.Pos
}

type pathUse struct {
	path string
	pos  token.Pos
}

func buildDurableMemo(pass *analysis.Pass, fn inspector.Cursor) *durableMemo {
	memo := &durableMemo{fn: fn, method: map[*types.Var][]token.Pos{}, arg: map[*types.Var][]token.Pos{}, opened: map[*types.Var]bool{}, storedVar: map[*types.Var][]*types.Var{}, storedField: map[*types.Var][]fieldStore{}, called: map[*types.Var][]pathUse{}, passed: map[*types.Var][]pathUse{}, used: map[*types.Var]bool{}}
	collectDurableCalls(pass, fn, memo)
	collectDurableAssigns(pass, fn, memo)
	collectDurableSpecs(pass, fn, memo)
	return memo
}

func collectDurableCalls(pass *analysis.Pass, fn inspector.Cursor, memo *durableMemo) {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		noteDurableCall(pass, cur, inspection.As[*ast.CallExpr](cur.Node()), memo)
	}
}

func noteDurableCall(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, memo *durableMemo) {
	noteMethodWrite(pass, call, memo)
	noteArgWrite(pass, call, memo)
	noteCalledOn(pass, call, memo)
	notePassedOn(pass, call, memo)
	noteUsedClose(pass, cur, call, memo)
}

func noteMethodWrite(pass *analysis.Pass, call *ast.CallExpr, memo *durableMemo) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 || !isWriteMethodName(sel.Sel.Name) {
		return
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		memo.method[v] = append(memo.method[v], call.Pos())
	}
}

func noteArgWrite(pass *analysis.Pass, call *ast.CallExpr, memo *durableMemo) {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return
	}
	sig, ok := fnobj.Type().(*types.Signature)
	if !ok {
		return
	}
	for idx, arg := range call.Args {
		id, ok := ast.Unparen(arg).(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		pt, ok := paramTypeAt(sig, idx, call)
		if !ok || !isWriterInterface(pt) {
			continue
		}
		memo.arg[v] = append(memo.arg[v], call.Pos())
	}
}

func noteCalledOn(pass *analysis.Pass, call *ast.CallExpr, memo *durableMemo) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	if v, path, ok := useOfVar(pass, sel.X); ok {
		memo.called[v] = append(memo.called[v], pathUse{path: path, pos: call.Pos()})
	}
}

func notePassedOn(pass *analysis.Pass, call *ast.CallExpr, memo *durableMemo) {
	for _, arg := range call.Args {
		if v, path, ok := useOfVar(pass, arg); ok {
			memo.passed[v] = append(memo.passed[v], pathUse{path: path, pos: call.Pos()})
		}
	}
}

func useOfVar(pass *analysis.Pass, e ast.Expr) (*types.Var, string, bool) {
	a := ast.Unparen(e)
	if u, ok := a.(*ast.UnaryExpr); ok && u.Op == token.AND {
		a = ast.Unparen(u.X)
	}
	if id, ok := a.(*ast.Ident); ok {
		if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
			return v, "", true
		}
		return nil, "", false
	}
	return rootFieldPath(pass, a)
}

func rootFieldPath(pass *analysis.Pass, lhs ast.Expr) (*types.Var, string, bool) {
	var fields []string
	cur := ast.Unparen(lhs)
	for {
		sel, ok := cur.(*ast.SelectorExpr)
		if !ok {
			break
		}
		fields = append([]string{sel.Sel.Name}, fields...)
		cur = ast.Unparen(sel.X)
	}
	id, ok := cur.(*ast.Ident)
	if !ok || len(fields) == 0 {
		return nil, "", false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, "", false
	}
	return v, strings.Join(fields, "."), true
}

func noteUsedClose(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, memo *durableMemo) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != closeMethodName && sel.Sel.Name != "Sync") {
		return
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !resultUsed(cur) {
		return
	}
	memo.used[v] = true
}

func collectDurableAssigns(pass *analysis.Pass, fn inspector.Cursor, memo *durableMemo) {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		noteDurableAssign(pass, inspection.As[*ast.AssignStmt](cur.Node()), memo)
	}
}

func noteDurableAssign(pass *analysis.Pass, as *ast.AssignStmt, memo *durableMemo) {
	for i, lhs := range as.Lhs {
		rhs := as.Rhs[0]
		if len(as.Rhs) == len(as.Lhs) {
			rhs = as.Rhs[i]
		}
		noteOpened(pass, lhs, rhs, memo)
		noteStoredAssign(pass, as, lhs, rhs, memo)
		noteStoredField(pass, as, lhs, rhs, memo)
	}
}

func noteOpened(pass *analysis.Pass, lhs, rhs ast.Expr, memo *durableMemo) {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !openWrites(pass, rhs) {
		return
	}
	memo.opened[v] = true
}

func noteStoredAssign(pass *analysis.Pass, as *ast.AssignStmt, lhs, rhs ast.Expr, memo *durableMemo) {
	if len(as.Lhs) != len(as.Rhs) && len(as.Rhs) != 1 {
		return
	}
	id, ok := ast.Unparen(rhs).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	if pass.TypesInfo.TypeOf(lhs) == nil || !isWriterInterface(pass.TypesInfo.TypeOf(lhs)) {
		return
	}
	wid, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	if w, ok := pass.TypesInfo.ObjectOf(wid).(*types.Var); ok {
		memo.storedVar[v] = append(memo.storedVar[v], w)
	}
}

func noteStoredField(pass *analysis.Pass, as *ast.AssignStmt, lhs, rhs ast.Expr, memo *durableMemo) {
	if len(as.Lhs) != len(as.Rhs) && len(as.Rhs) != 1 {
		return
	}
	id, ok := ast.Unparen(rhs).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	t := pass.TypesInfo.TypeOf(lhs)
	if t == nil || !isWriterInterface(t) {
		return
	}
	w, fpath, ok := rootFieldPath(pass, lhs)
	if !ok {
		return
	}
	if !fieldDeclaredAbroad(pass, lhs) {
		return
	}
	memo.storedField[v] = append(memo.storedField[v], fieldStore{root: w, path: fpath, pos: as.Pos()})
}

func fieldDeclaredAbroad(pass *analysis.Pass, lhs ast.Expr) bool {
	sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	choice, ok := pass.TypesInfo.Selections[sel]
	if !ok || choice == nil {
		return false
	}
	if _, ok := choice.Obj().(*types.Var); !ok {
		return false
	}
	return selectionDeclAbroad(choice, pass.Pkg)
}

func selectionDeclAbroad(choice *types.Selection, here *types.Package) bool {
	t := types.Unalias(choice.Recv())
	for _, index := range choice.Index()[:len(choice.Index())-1] {
		var ok bool
		t, ok = descendField(t, index)
		if !ok {
			return false
		}
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	pkg := named.Obj().Pkg()
	return pkg != nil && pkg != here
}

func descendField(t types.Type, index int) (types.Type, bool) {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	st, ok := types.Unalias(t).Underlying().(*types.Struct)
	if !ok || index < 0 || index >= st.NumFields() {
		return nil, false
	}
	return st.Field(index).Type(), true
}

func collectDurableSpecs(pass *analysis.Pass, fn inspector.Cursor, memo *durableMemo) {
	for cur := range fn.Preorder((*ast.ValueSpec)(nil)) {
		if owner(cur) != fn {
			continue
		}
		noteDurableSpec(pass, inspection.As[*ast.ValueSpec](cur.Node()), memo)
	}
}

func noteDurableSpec(pass *analysis.Pass, vs *ast.ValueSpec, memo *durableMemo) {
	if len(vs.Values) == 0 {
		return
	}
	for i, name := range vs.Names {
		v, ok := valueForName(vs, i)
		if !ok {
			continue
		}
		id, ok := ast.Unparen(v).(*ast.Ident)
		if !ok {
			continue
		}
		x, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		obj, ok := pass.TypesInfo.Defs[name].(*types.Var)
		if !ok || !isWriterInterface(obj.Type()) {
			continue
		}
		memo.storedVar[x] = append(memo.storedVar[x], obj)
	}
}

func checkDurableSites(pass *analysis.Pass, fn inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	memo := buildDurableMemo(pass, fn)
	for cur := range fn.Preorder((*ast.ExprStmt)(nil), (*ast.DeferStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		checkOneDurableSite(pass, cur, memo, f, hash, reported)
	}
}

func checkOneDurableSite(pass *analysis.Pass, cur inspector.Cursor, memo *durableMemo, f facts, hash *types.Interface, reported map[token.Pos]bool) {
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
	if isExempt(pass, call, f, hash) {
		return
	}
	if !durableHolds(pass, cur, xobj, memo) {
		return
	}
	pass.Reportf(call.Pos(), "%s is written in this function and the error of %s.Close is dropped, so a failed Close can lose written data: return or check the Close error", xname, xname)
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

func durableHolds(pass *analysis.Pass, closeCur inspector.Cursor, x *types.Var, memo *durableMemo) bool {
	if !isLocalPtrStruct(memo.fn, x) {
		return false
	}
	if isConnection(x.Type()) {
		return false
	}
	guard := guardWriteCall(pass, memo.fn, closeCur, x)
	if !writtenExcept(x, guard, memo) {
		return false
	}
	return !memo.used[x]
}

func writtenExcept(x *types.Var, ignore *ast.CallExpr, memo *durableMemo) bool {
	if hasMethodWrite(x, ignore, memo) {
		return true
	}
	if hasArgWrite(x, ignore, memo) {
		return true
	}
	return storedVarWritten(x, memo) || storedFieldWritten(x, memo)
}

func storedVarWritten(x *types.Var, memo *durableMemo) bool {
	if !memo.opened[x] {
		return false
	}
	for _, w := range memo.storedVar[x] {
		if len(memo.called[w]) > 0 || len(memo.arg[w]) > 0 {
			return true
		}
	}
	return false
}

func storedFieldWritten(x *types.Var, memo *durableMemo) bool {
	if !memo.opened[x] {
		return false
	}
	for _, s := range memo.storedField[x] {
		if rootUsedAfter(s, memo) {
			return true
		}
	}
	return false
}

func rootUsedAfter(s fieldStore, memo *durableMemo) bool {
	return pathUsedAfter(memo.called[s.root], s) || pathUsedAfter(memo.passed[s.root], s)
}

func pathUsedAfter(uses []pathUse, s fieldStore) bool {
	for _, u := range uses {
		if u.pos > s.pos && (u.path == "" || pathHasPrefix(s.path, u.path)) {
			return true
		}
	}
	return false
}

func hasMethodWrite(x *types.Var, ignore *ast.CallExpr, memo *durableMemo) bool {
	for _, pos := range memo.method[x] {
		if ignore == nil || pos != ignore.Pos() {
			return true
		}
	}
	return false
}

func hasArgWrite(x *types.Var, ignore *ast.CallExpr, memo *durableMemo) bool {
	for _, pos := range memo.arg[x] {
		if ignore == nil || pos != ignore.Pos() {
			return true
		}
	}
	return false
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

func guardWriteCall(pass *analysis.Pass, fn, closeCur inspector.Cursor, x *types.Var) *ast.CallExpr {
	ifStmt, ok := enclosingFailureIf(closeCur)
	if !ok {
		return nil
	}
	errVar, errOk := failureErrVar(pass, ifStmt)
	if !errOk {
		return nil
	}
	if call, initOk := initWriteCall(pass, ifStmt.Init, errVar, x); initOk {
		return call
	}
	call, prevOk := prevWriteCall(pass, fn, ifStmt, errVar, x)
	if !prevOk {
		return nil
	}
	return call
}

func enclosingFailureIf(closeCur inspector.Cursor) (*ast.IfStmt, bool) {
	pos := closeCur.Node().Pos()
	for cur := closeCur.Parent(); cur.Node() != nil; cur = cur.Parent() {
		ifStmt, ok := cur.Node().(*ast.IfStmt)
		if !ok {
			continue
		}
		if ifStmt.Body != nil && ifStmt.Body.Pos() <= pos && pos < ifStmt.Body.End() {
			return ifStmt, true
		}
	}
	return nil, false
}

func failureErrVar(pass *analysis.Pass, ifStmt *ast.IfStmt) (*types.Var, bool) {
	cond, ok := ast.Unparen(ifStmt.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return nil, false
	}
	if id, ok := errIdent(pass, cond.X, cond.Y); ok {
		return id, true
	}
	return errIdent(pass, cond.Y, cond.X)
}

func errIdent(pass *analysis.Pass, e, other ast.Expr) (*types.Var, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || !isNilIdent(pass.TypesInfo, other) {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	return v, true
}

func initWriteCall(pass *analysis.Pass, init ast.Stmt, errVar, x *types.Var) (*ast.CallExpr, bool) {
	as, ok := init.(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	return assignWriteCall(pass, as, errVar, x)
}

func prevWriteCall(pass *analysis.Pass, fn inspector.Cursor, ifStmt *ast.IfStmt, errVar, x *types.Var) (*ast.CallExpr, bool) {
	block, ok := enclosingBlock(fn, ifStmt)
	if !ok {
		return nil, false
	}
	for i, stmt := range block {
		if stmt.Pos() != ifStmt.Pos() {
			continue
		}
		if i == 0 {
			return nil, false
		}
		as, ok := block[i-1].(*ast.AssignStmt)
		if !ok {
			return nil, false
		}
		return assignWriteCall(pass, as, errVar, x)
	}
	return nil, false
}

func assignWriteCall(pass *analysis.Pass, as *ast.AssignStmt, errVar, x *types.Var) (*ast.CallExpr, bool) {
	for i, lhs := range as.Lhs {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(errVar) {
			continue
		}
		rhs := as.Rhs[0]
		if len(as.Rhs) == len(as.Lhs) {
			rhs = as.Rhs[i]
		}
		call, ok := ast.Unparen(rhs).(*ast.CallExpr)
		if !ok || !isWriteEvidence(pass, call, x) {
			continue
		}
		return call, true
	}
	return nil, false
}

func enclosingBlock(fn inspector.Cursor, target *ast.IfStmt) ([]ast.Stmt, bool) {
	for cur := range fn.Preorder((*ast.BlockStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		block := inspection.As[*ast.BlockStmt](cur.Node())
		for _, stmt := range block.List {
			if stmt.Pos() == target.Pos() {
				return block.List, true
			}
		}
	}
	return nil, false
}

func isWriteEvidence(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	if isWriteMethodOn(pass, call, x) {
		return true
	}
	return callWritesViaWriter(pass, call, x)
}

func isWriteMethodOn(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 || !isWriteMethodName(sel.Sel.Name) {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(x)
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

func openWrites(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn == nil {
		return false
	}
	switch fn.FullName() {
	case "os.Create", "os.CreateTemp":
		return true
	case "os.OpenFile":
		return openFlagWritable(pass, call)
	}
	return false
}

func openFlagWritable(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	tv, ok := pass.TypesInfo.Types[call.Args[1]]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return false
	}
	flag, exact := constant.Int64Val(tv.Value)
	return exact && flag&3 != 0
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
