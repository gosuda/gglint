package resstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func hasCloseRelease(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	return isCloseOf(pass, call, v)
}

func isCloseOf(pass *analysis.Pass, call *ast.CallExpr, v *types.Var) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return false
	}
	if len(call.Args) != 0 {
		return false
	}
	recv, ok := closeRecv(pass, call)
	if !ok {
		return false
	}
	if !exactVar(pass, recv, v) {
		return false
	}
	return closeOwns(pass, call)
}

func closeOwns(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	pkg := fn.Pkg()
	if pkg == nil {
		return false
	}
	return closeFailPkgs[pkg.Path()]
}

func closeRecv(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	s := pass.TypesInfo.Selections[sel]
	if s == nil {
		return nil, false
	}
	if s.Kind() == types.MethodExpr {
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	}
	if s.Kind() != types.MethodVal {
		return nil, false
	}
	return sel.X, true
}

func exactVar(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	k, ok := flow.KeyOf(pass.TypesInfo, e)
	return ok && k.Root == v && k.Path == ""
}

func hasPutRelease(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Put" {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	if !isPoolType(pass.TypesInfo.Types[sel.X].Type) {
		return false
	}
	return exactVar(pass, call.Args[0], v)
}

func hasFreeRelease(pass *analysis.Pass, fnode, n ast.Node, v *types.Var) bool {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	if handled, release := freeLitRelease(pass, call, v); handled {
		return release
	}
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	if fn.Name() == "_Cfunc_free" {
		return freeArgsMention(pass, fnode, call, v)
	}
	pkg := fn.Pkg()
	if pkg == nil {
		return false
	}
	if fn.Name() != "Munmap" {
		return false
	}
	return freeMunmapByPkg(pass, fnode, pkg, call, v)
}

func freeMunmapByPkg(pass *analysis.Pass, fnode ast.Node, pkg *types.Package, call *ast.CallExpr, v *types.Var) bool {
	if pkg.Path() != "syscall" && pkg.Path() != "golang.org/x/sys/unix" {
		return false
	}
	return freeArgsMention(pass, fnode, call, v)
}

func freeLitRelease(pass *analysis.Pass, call *ast.CallExpr, v *types.Var) (release, handled bool) {
	lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
	if !ok || len(call.Args) != 0 || lit.Body == nil {
		return false, false
	}
	return litFreeRelease(pass, lit, v), true
}

func litFreeRelease(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) bool {
	for _, s := range lit.Body.List {
		if litFreeFound(pass, lit, s, v) {
			return true
		}
	}
	return false
}

func litFreeFound(pass *analysis.Pass, lit *ast.FuncLit, x ast.Node, v *types.Var) bool {
	es, ok := x.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	if _, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		return false
	}
	cfn := typeutil.StaticCallee(pass.TypesInfo, call)
	if cfn == nil || cfn.Name() != "_Cfunc_free" {
		return false
	}
	return freeArgsMention(pass, lit, call, v)
}

func freeArgsMention(pass *analysis.Pass, fn ast.Node, call *ast.CallExpr, v *types.Var) bool {
	if len(call.Args) == 0 {
		return false
	}
	return freeTargets(pass, fn, call.Args[0], v)
}

func freeTargets(pass *analysis.Pass, fn ast.Node, e ast.Expr, v *types.Var) bool {
	e = ast.Unparen(e)
	if exactVar(pass, e, v) {
		return true
	}
	switch x := e.(type) {
	case *ast.SliceExpr:
		if !wholeSlice(pass, x, v) {
			return false
		}
		return freeTargets(pass, fn, x.X, v)
	case *ast.CallExpr:
		return freeConvTarget(pass, fn, e, x, v)
	case *ast.Ident:
		return freeTempTarget(pass, fn, x, v)
	}
	return false
}

func wholeSlice(pass *analysis.Pass, x *ast.SliceExpr, v *types.Var) bool {
	if x.X == nil || x.Max != nil {
		return false
	}
	if !exactVar(pass, x.X, v) {
		return false
	}
	if x.Low != nil && !isZeroExpr(pass, x.Low) {
		return false
	}
	if x.High == nil {
		return true
	}
	return isLenOf(pass, x.High, v)
}

func isZeroExpr(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[ast.Unparen(e)]
	if !ok {
		return false
	}
	return tv.Value != nil && tv.Value.String() == "0"
}

func isLenOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if !plainCall(pass, call) {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != lenBuiltin {
		return false
	}
	return exactVar(pass, call.Args[0], v)
}

func freeConvTarget(pass *analysis.Pass, fn ast.Node, e ast.Expr, call *ast.CallExpr, v *types.Var) bool {
	if len(call.Args) != 1 || !pass.TypesInfo.Types[call.Fun].IsType() {
		return false
	}
	if t := pass.TypesInfo.Types[e].Type; t == nil || t.String() != "unsafe.Pointer" {
		return false
	}
	return freeTargets(pass, fn, call.Args[0], v)
}

func freeTempTarget(pass *analysis.Pass, scope ast.Node, id *ast.Ident, v *types.Var) bool {
	tv, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok || tv == v {
		return false
	}
	def, ok := freeTempDef(pass, scope, tv)
	if !ok {
		return false
	}
	return freeConvOf(pass, def, v) || exactVar(pass, def, v)
}

func freeTempDef(pass *analysis.Pass, scope ast.Node, tv *types.Var) (ast.Expr, bool) {
	var rhs ast.Expr
	unique := true
	ast.Inspect(scope, func(n ast.Node) bool {
		if !unique || freeTempSkip(n, scope) {
			return false
		}
		as, targeted := freeTempTargeted(pass, n, tv)
		if !targeted {
			return true
		}
		if rhs != nil || !freeTempSingle(as) {
			unique = false
			return false
		}
		rhs = as.Rhs[0]
		return true
	})
	if !unique || rhs == nil {
		return nil, false
	}
	return rhs, true
}

func freeTempSkip(n, scope ast.Node) bool {
	lit, ok := n.(*ast.FuncLit)
	return ok && lit != scope
}

func freeTempTargeted(pass *analysis.Pass, n ast.Node, tv *types.Var) (*ast.AssignStmt, bool) {
	as, ok := n.(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	for _, lhs := range as.Lhs {
		lid, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		if tempBinds(pass, as, lid, tv) {
			return as, true
		}
	}
	return nil, false
}

func tempBinds(pass *analysis.Pass, as *ast.AssignStmt, lid *ast.Ident, tv *types.Var) bool {
	if v, ok := pass.TypesInfo.Defs[lid].(*types.Var); ok && v == tv {
		return true
	}
	if v, ok := pass.TypesInfo.Uses[lid].(*types.Var); ok && v == tv {
		return true
	}
	return freeTempVar(pass, as, lid) == tv
}

func freeTempSingle(as *ast.AssignStmt) bool {
	return as.Tok == token.DEFINE && len(as.Rhs) == 1
}

func freeTempVar(pass *analysis.Pass, as *ast.AssignStmt, lid *ast.Ident) *types.Var {
	if as.Tok == token.DEFINE {
		if w, ok := pass.TypesInfo.Defs[lid].(*types.Var); ok {
			return w
		}
		return nil
	}
	if w, ok := pass.TypesInfo.Uses[lid].(*types.Var); ok {
		return w
	}
	return nil
}

func freeConvOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !pass.TypesInfo.Types[call.Fun].IsType() {
		return false
	}
	if t := pass.TypesInfo.Types[e].Type; t == nil || t.String() != "unsafe.Pointer" {
		return false
	}
	return exactVar(pass, call.Args[0], v)
}

func methodCallsOn(pass *analysis.Pass, n ast.Node, v *types.Var) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(n, func(x ast.Node) bool {
		if _, isLit := x.(*ast.FuncLit); isLit {
			return false
		}
		call, isCall := x.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if _, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); !isSel {
			return true
		}
		recv, hasRecv := closeRecv(pass, call)
		if !hasRecv {
			return true
		}
		if !exactVar(pass, recv, v) {
			return true
		}
		out = append(out, call)
		return true
	})
	return out
}

const (
	closeMethodRead        = "Read"
	closeMethodWrite       = "Write"
	closeMethodWriteString = "WriteString"
	lenBuiltin             = "len"
	capBuiltin             = "cap"
)

var closeFailPkgs = map[string]bool{"os": true, "net": true, "net/http": true}

var closeFailMethods = map[string]bool{
	closeMethodRead: true, "ReadAt": true, closeMethodWrite: true, "WriteAt": true, closeMethodWriteString: true,
	"Seek": true, "Sync": true, "Truncate": true, "Accept": true, "RoundTrip": true,
	"SetDeadline": true, "SetReadDeadline": true, "SetWriteDeadline": true,
}

func closeFailsOn(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !closeFailMethods[callName(call)] {
		return false
	}
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	pkg := fn.Pkg()
	if pkg == nil {
		return false
	}
	return closeFailPkgs[pkg.Path()]
}

func callName(call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

func redefinesVar(pass *analysis.Pass, fn, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		if !lhsAssigns(pass, x.Lhs, v) {
			return false
		}
		for _, r := range x.Rhs {
			if exprMentions(pass, r, v) {
				return false
			}
		}
		return true
	case *ast.Ident:
		return assignsVar(pass, fn, n, v)
	}
	return false
}

func assignsVar(pass *analysis.Pass, fn, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return lhsAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return incAssigns(pass, x.X, v)
	case *ast.Ident:
		if pass.TypesInfo.Uses[x] != v {
			return false
		}
		return rangeTarget(fn, x)
	case *ast.ValueSpec:
		return false
	}
	return false
}

func lhsAssigns(pass *analysis.Pass, lhs []ast.Expr, v *types.Var) bool {
	for _, e := range lhs {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.Uses[id] == v {
			return true
		}
	}
	return false
}

func incAssigns(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	return pass.TypesInfo.Uses[id] == v
}

func rangeTarget(fn ast.Node, id *ast.Ident) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		r, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if r.Key == id || r.Value == id {
			found = true
		}
		return !found
	})
	return found
}

func mentionsVar(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if pruneMention(pass, x) {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}

func pruneMention(pass *analysis.Pass, x ast.Node) bool {
	if _, ok := x.(*ast.FuncLit); ok {
		return true
	}
	if _, ok := x.(*ast.SliceExpr); ok {
		return true
	}
	if call, ok := x.(*ast.CallExpr); ok && lenCapCall(pass, call) {
		return true
	}
	if bin, ok := x.(*ast.BinaryExpr); ok && nilCompare(pass, bin) {
		return true
	}
	return false
}

func lenCapCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	if id.Name != lenBuiltin && id.Name != capBuiltin {
		return false
	}
	if _, ok = pass.TypesInfo.Uses[id].(*types.Builtin); !ok {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	_, ok = ast.Unparen(call.Args[0]).(*ast.Ident)
	return ok
}

func nilCompare(pass *analysis.Pass, bin *ast.BinaryExpr) bool {
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return false
	}
	xNil := pass.TypesInfo.Types[bin.X].IsNil()
	yNil := pass.TypesInfo.Types[bin.Y].IsNil()
	if xNil == yNil {
		return false
	}
	if xNil {
		_, ok := ast.Unparen(bin.Y).(*ast.Ident)
		return ok
	}
	_, ok := ast.Unparen(bin.X).(*ast.Ident)
	return ok
}

func exprMentions(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	found := false
	ast.Inspect(e, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}

func mentionPos(pass *analysis.Pass, n ast.Node, v *types.Var) token.Pos {
	pos := token.NoPos
	ast.Inspect(n, func(x ast.Node) bool {
		if pos != token.NoPos {
			return false
		}
		if pruneMention(pass, x) {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			pos = id.Pos()
		}
		return pos == token.NoPos
	})
	if pos != token.NoPos {
		return pos
	}
	return n.Pos()
}

func escapesVar(pass *analysis.Pass, fn, n ast.Node, v *types.Var) bool {
	if takesAddr(pass, n, v) && !discardedAddr(n) {
		return true
	}
	if callArgEscapes(pass, n, v) {
		return true
	}
	if returnEscapes(pass, n, v) {
		return true
	}
	if sendEscapes(pass, n, v) {
		return true
	}
	if storeEscapes(pass, fn, n, v) {
		return true
	}
	return litCaptures(pass, n, v)
}

func discardedAddr(n ast.Node) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, l := range as.Lhs {
		if id, ok := ast.Unparen(l).(*ast.Ident); !ok || id.Name != "_" {
			return false
		}
	}
	return true
}

func takesAddr(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		u, ok := x.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		k, ok := flow.KeyOf(pass.TypesInfo, u.X)
		if ok && k.Root == v {
			found = true
		}
		return !found
	})
	return found
}

func callArgEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if plainCall(pass, call) {
			return true
		}
		for _, a := range call.Args {
			if exactVar(pass, a, v) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func plainCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	if pass.TypesInfo.Types[call.Fun].IsType() {
		return true
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	if id.Name != lenBuiltin && id.Name != capBuiltin {
		return false
	}
	_, ok = pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok
}

func returnEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	ret, ok := n.(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, r := range ret.Results {
		if exprMentions(pass, r, v) {
			return true
		}
	}
	return false
}

func sendEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	s, ok := n.(*ast.SendStmt)
	if !ok {
		return false
	}
	return exprMentions(pass, s.Value, v)
}

func storeEscapes(pass *analysis.Pass, fn, n ast.Node, v *types.Var) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok {
		return false
	}
	if !storeTargetEscapes(pass, fn, as.Lhs) {
		return false
	}
	for _, r := range as.Rhs {
		if exactVar(pass, r, v) {
			return true
		}
	}
	return false
}

func storeTargetEscapes(pass *analysis.Pass, fn ast.Node, lhs []ast.Expr) bool {
	for _, l := range lhs {
		switch ast.Unparen(l).(type) {
		case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
			return true
		case *ast.Ident:
			if id, ok := ast.Unparen(l).(*ast.Ident); ok && globalVar(pass, fn, id) {
				return true
			}
		}
	}
	return false
}

func globalVar(pass *analysis.Pass, fn ast.Node, id *ast.Ident) bool {
	var vv *types.Var
	if o, ok := pass.TypesInfo.Defs[id].(*types.Var); ok {
		vv = o
	} else if o, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
		vv = o
	} else {
		return false
	}
	p := vv.Pos()
	return p < fn.Pos() || p >= fn.End()
}

func litCaptures(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		lit, ok := x.(*ast.FuncLit)
		if !ok {
			return true
		}
		if lit.Body == nil {
			return false
		}
		if !litMentions(pass, lit, v) {
			return false
		}
		if calledInPlace(n, lit) {
			return false
		}
		found = true
		return false
	})
	return found
}

func litMentions(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) bool {
	found := false
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if found {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}

func calledInPlace(n ast.Node, lit *ast.FuncLit) bool {
	if _, ok := n.(*ast.GoStmt); ok {
		return false
	}
	if _, ok := n.(*ast.DeferStmt); ok {
		return false
	}
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ast.Unparen(call.Fun) == lit {
			found = true
		}
		return !found
	})
	return found
}
