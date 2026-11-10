package errdrop

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

func checkVerdictLists(pass *analysis.Pass, fn inspector.Cursor, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	sig := fnSignature(pass, fn)
	if sig == nil || sig.Results() == nil || sig.Results().Len() == 0 {
		return
	}
	if !isError(sig.Results().At(sig.Results().Len() - 1).Type()) {
		return
	}
	for cur := range fn.Preorder((*ast.BlockStmt)(nil), (*ast.CaseClause)(nil), (*ast.CommClause)(nil)) {
		if owner(cur) != fn {
			continue
		}
		list := verdictListOf(cur)
		if len(list) < 2 {
			continue
		}
		checkVerdictPairs(pass, fn, list, f, hash, reported)
	}
}

func verdictListOf(cur inspector.Cursor) []ast.Stmt {
	switch n := cur.Node().(type) {
	case *ast.BlockStmt:
		return n.List
	case *ast.CaseClause:
		return n.Body
	case *ast.CommClause:
		return n.Body
	}
	return nil
}

type verdictPair struct {
	list []ast.Stmt
	idx  int
}

func checkVerdictPairs(pass *analysis.Pass, fn inspector.Cursor, list []ast.Stmt, f facts, hash *types.Interface, reported map[token.Pos]bool) {
	for i := range len(list) - 1 {
		call, ok := verdictPairCall(pass, fn, verdictPair{list: list, idx: i}, f, hash, reported)
		if !ok {
			continue
		}
		callee := types.ExprString(call.Fun)
		pass.Reportf(call.Pos(), "the error of %s is dropped and the next statement returns nil, so the caller is told this step succeeded: return the error, or write _ = %s(...) to state that the step is best effort", callee, callee)
	}
}

func verdictPairCall(pass *analysis.Pass, fn inspector.Cursor, p verdictPair, f facts, hash *types.Interface, reported map[token.Pos]bool) (*ast.CallExpr, bool) {
	es, ok := p.list[p.idx].(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call := callOf(es.X)
	if call == nil || !isRealCall(pass.TypesInfo, call) {
		return nil, false
	}
	if verdictSuppressed(pass, fn, call, f, hash, reported) {
		return nil, false
	}
	if !verdictNextReturnsNil(pass, p.list[p.idx+1]) {
		return nil, false
	}
	return call, true
}

func verdictSuppressed(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr, f facts, hash *types.Interface, reported map[token.Pos]bool) bool {
	if isCloseMethod(pass.TypesInfo, call) {
		return true
	}
	if !dropsError(pass.TypesInfo, call) {
		return true
	}
	if reported[call.Pos()] {
		return true
	}
	if isExempt(pass, call, f.never, hash) {
		return true
	}
	if isStickyCall(pass, call, f.sticky) {
		return true
	}
	if unreadAfterRead(pass, fn, call) {
		return true
	}
	return discardAfterPeek(pass, fn, call)
}

func isStickyCall(pass *analysis.Pass, call *ast.CallExpr, stickySet map[*types.Func]bool) bool {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn == nil {
		return false
	}
	origin := originOf(fn)
	if stickySet[origin] {
		return true
	}
	var found sticky
	return pass.ImportObjectFact(origin, &found)
}

func unreadAfterRead(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	want, ok := map[string]string{"UnreadByte": "ReadByte", "UnreadRune": "ReadRune"}[sel.Sel.Name]
	if !ok {
		return false
	}
	recv := types.ExprString(sel.X)
	read, ok := nearestRecvCall(fn, recv, call.Pos())
	if !ok {
		return false
	}
	if read.Sel.Sel.Name != want {
		return false
	}
	return readErrTested(pass, fn, read.Call, call.Pos())
}

type recvCall struct {
	Call *ast.CallExpr
	Sel  *ast.SelectorExpr
}

func nearestRecvCall(fn inspector.Cursor, recv string, before token.Pos) (recvCall, bool) {
	var best recvCall
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		c := inspection.As[*ast.CallExpr](cur.Node())
		if c.Pos() >= before {
			continue
		}
		sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
		if !ok || types.ExprString(sel.X) != recv {
			continue
		}
		if best.Call == nil || c.Pos() > best.Call.Pos() {
			best = recvCall{Call: c, Sel: sel}
		}
	}
	return best, best.Call != nil
}

func readErrTested(pass *analysis.Pass, fn inspector.Cursor, read *ast.CallExpr, before token.Pos) bool {
	v, ok := readErrVar(pass, fn, read)
	if !ok {
		return false
	}
	return errTestedBetween(pass, fn, v, read.Pos(), before)
}

func readErrVar(pass *analysis.Pass, fn inspector.Cursor, read *ast.CallExpr) (*types.Var, bool) {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		as := inspection.As[*ast.AssignStmt](cur.Node())
		if !assignHasCall(as, read) {
			continue
		}
		return trailingErrVar(pass, as)
	}
	return nil, false
}

func assignHasCall(as *ast.AssignStmt, read *ast.CallExpr) bool {
	for _, rhs := range as.Rhs {
		if ast.Unparen(rhs) == ast.Unparen(read) {
			return true
		}
	}
	return false
}

func trailingErrVar(pass *analysis.Pass, as *ast.AssignStmt) (*types.Var, bool) {
	if len(as.Lhs) < 2 {
		return nil, false
	}
	id, ok := ast.Unparen(as.Lhs[len(as.Lhs)-1]).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !isError(v.Type()) {
		return nil, false
	}
	return v, true
}

func errTestedBetween(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, from, to token.Pos) bool {
	for cur := range fn.Preorder((*ast.IfStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		ifs := inspection.As[*ast.IfStmt](cur.Node())
		if ifs.Pos() <= from || ifs.Pos() >= to {
			continue
		}
		if condTestsErr(pass, ifs.Cond, v) {
			return true
		}
	}
	return false
}

func condTestsErr(pass *analysis.Pass, cond ast.Expr, v *types.Var) bool {
	mentions := false
	comparesNil := false
	ast.Inspect(cond, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			mentions = true
		}
		if id.Name == nilName {
			comparesNil = true
		}
		return true
	})
	return mentions && comparesNil
}

func verdictNextReturnsNil(pass *analysis.Pass, stmt ast.Stmt) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	return isNilIdent(pass.TypesInfo, ret.Results[len(ret.Results)-1])
}

func isCloseMethod(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != closeMethodName {
		return false
	}
	if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
		if _, ok := info.ObjectOf(id).(*types.PkgName); ok {
			return false
		}
	}
	return true
}

func discardAfterPeek(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr) bool {
	recv, d, ok := discardTarget(pass, call)
	if !ok {
		return false
	}
	peek := peekBefore(pass, fn, recv, d, call.Pos())
	if !peek.IsValid() {
		return false
	}
	return windowClean(fn, recv, peek, call.Pos())
}

func discardTarget(pass *analysis.Pass, call *ast.CallExpr) (recvText string, n int64, ok bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Discard" || len(call.Args) != 1 {
		return "", 0, false
	}
	if !isBufioReader(pass, sel.X) {
		return "", 0, false
	}
	d, ok := constIntArg(pass, call.Args[0])
	if !ok {
		return "", 0, false
	}
	return types.ExprString(sel.X), d, true
}

func isBufioReader(pass *analysis.Pass, e ast.Expr) bool {
	ptr, ok := types.Unalias(pass.TypesInfo.TypeOf(e)).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "bufio" && named.Obj().Name() == "Reader"
}

func constIntArg(pass *analysis.Pass, e ast.Expr) (int64, bool) {
	v := pass.TypesInfo.Types[e].Value
	if v == nil || v.Kind() != constant.Int {
		return 0, false
	}
	d, exact := constant.Int64Val(v)
	if !exact {
		return 0, false
	}
	return d, true
}

func peekBefore(pass *analysis.Pass, fn inspector.Cursor, recv string, d int64, before token.Pos) token.Pos {
	var at token.Pos
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		as := inspection.As[*ast.AssignStmt](cur.Node())
		if as.Pos() >= before {
			break
		}
		if peekFills(pass, fn, as, recv, d) {
			at = as.Pos()
		}
	}
	return at
}

func peekFills(pass *analysis.Pass, fn inspector.Cursor, as *ast.AssignStmt, recv string, d int64) bool {
	if len(as.Rhs) != 1 {
		return false
	}
	call := callOf(as.Rhs[0])
	if call == nil {
		return false
	}
	p, ok := peekCallArg(pass, call, recv)
	if !ok || p < d {
		return false
	}
	return peekErrTracked(pass, fn, as)
}

func peekCallArg(pass *analysis.Pass, call *ast.CallExpr, recv string) (int64, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Peek" || types.ExprString(sel.X) != recv || len(call.Args) != 1 {
		return 0, false
	}
	return constIntArg(pass, call.Args[0])
}

func peekErrTracked(pass *analysis.Pass, fn inspector.Cursor, as *ast.AssignStmt) bool {
	if len(as.Lhs) < 2 {
		return false
	}
	id, ok := ast.Unparen(as.Lhs[len(as.Lhs)-1]).(*ast.Ident)
	if !ok || id.Name == "_" {
		return false
	}
	obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return false
	}
	return usedAfter(pass, fn, obj, as.Pos())
}

func usedAfter(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, after token.Pos) bool {
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if id.Pos() <= after {
			continue
		}
		if pass.TypesInfo.Uses[id] == v {
			return true
		}
	}
	return false
}

func windowClean(fn inspector.Cursor, recv string, after, before token.Pos) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		c := inspection.As[*ast.CallExpr](cur.Node())
		if c.Pos() <= after || c.Pos() >= before {
			continue
		}
		if sameRecvCall(c, recv) {
			return false
		}
		for _, arg := range c.Args {
			if types.ExprString(arg) == recv {
				return false
			}
		}
	}
	return true
}

func sameRecvCall(call *ast.CallExpr, recv string) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || types.ExprString(sel.X) != recv {
		return false
	}
	switch sel.Sel.Name {
	case "Peek", "Buffered", "Size":
		return false
	}
	return true
}
