package ctxflow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const cancelMessage = "the deferred cancel runs before the caller uses the returned context, so the context is already cancelled: return the cancel function instead"

var cancelAnalyzer = &analysis.Analyzer{
	Name:     "cancelreturned",
	Doc:      "report a deferred cancel that runs before the caller uses the returned context",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runCancel,
}

func runCancel(pass *analysis.Pass) (any, error) {
	for fn := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkCancel(pass, fn)
	}
	return nil, nil
}

type cancelPair struct {
	ctx    *types.Var
	cancel *types.Var
}

func checkCancel(pass *analysis.Pass, fn inspector.Cursor) {
	for _, p := range collectPairs(pass, fn) {
		checkPair(pass, fn, p)
	}
}

func checkPair(pass *analysis.Pass, fn inspector.Cursor, p cancelPair) {
	site, ok := deferredCancel(pass, fn, p.cancel)
	if !ok {
		return
	}
	if mentionsVar(pass, fn, p.cancel) || !mentionsVar(pass, fn, p.ctx) {
		return
	}
	pass.Report(analysis.Diagnostic{Pos: site.Pos(), End: site.End(), Message: cancelMessage})
}

func deferredCancel(pass *analysis.Pass, fn inspector.Cursor, cancel *types.Var) (*ast.DeferStmt, bool) {
	for cur := range fn.Preorder((*ast.DeferStmt)(nil)) {
		if ownerOf(cur) != fn {
			continue
		}
		if d, ok := cur.Node().(*ast.DeferStmt); ok && isCancelCall(pass, d, cancel) {
			return d, true
		}
	}
	return nil, false
}

func collectPairs(pass *analysis.Pass, fn inspector.Cursor) []cancelPair {
	var out []cancelPair
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.DeclStmt)(nil)) {
		if ownerOf(cur) != fn {
			continue
		}
		switch n := cur.Node().(type) {
		case *ast.AssignStmt:
			if p, ok := pairFromAssign(pass, n); ok {
				out = append(out, p)
			}
		case *ast.DeclStmt:
			out = append(out, pairsFromDecl(pass, n)...)
		}
	}
	return out
}

func pairFromAssign(pass *analysis.Pass, as *ast.AssignStmt) (cancelPair, bool) {
	if len(as.Lhs) != 2 || len(as.Rhs) != 1 {
		return cancelPair{}, false
	}
	call, ok := stripParens(as.Rhs[0]).(*ast.CallExpr)
	if !ok || !isWithCall(pass.TypesInfo, call) {
		return cancelPair{}, false
	}
	return pairFromIdents(pass, as.Lhs[0], as.Lhs[1])
}

func pairFromIdents(pass *analysis.Pass, x, y ast.Expr) (cancelPair, bool) {
	xid, ok := stripParens(x).(*ast.Ident)
	if !ok {
		return cancelPair{}, false
	}
	yid, ok := stripParens(y).(*ast.Ident)
	if !ok {
		return cancelPair{}, false
	}
	ctx := varOf(pass.TypesInfo, xid)
	cancel := varOf(pass.TypesInfo, yid)
	if ctx == nil || cancel == nil {
		return cancelPair{}, false
	}
	return cancelPair{ctx: ctx, cancel: cancel}, true
}

func pairsFromDecl(pass *analysis.Pass, d *ast.DeclStmt) []cancelPair {
	gd, ok := d.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return nil
	}
	var out []cancelPair
	for _, spec := range gd.Specs {
		if p, ok := pairFromValueSpec(pass, spec); ok {
			out = append(out, p)
		}
	}
	return out
}

func pairFromValueSpec(pass *analysis.Pass, spec ast.Spec) (cancelPair, bool) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) != 2 || len(vs.Values) != 1 {
		return cancelPair{}, false
	}
	call, ok := stripParens(vs.Values[0]).(*ast.CallExpr)
	if !ok || !isWithCall(pass.TypesInfo, call) {
		return cancelPair{}, false
	}
	ctx := varOf(pass.TypesInfo, vs.Names[0])
	cancel := varOf(pass.TypesInfo, vs.Names[1])
	if ctx == nil || cancel == nil {
		return cancelPair{}, false
	}
	return cancelPair{ctx: ctx, cancel: cancel}, true
}

func isWithCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil {
		return false
	}
	if pkg := fn.Pkg(); pkg == nil || pkg.Path() != contextPath {
		return false
	}
	switch fn.Name() {
	case "WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause", "WithTimeoutCause", "WithDeadlineCause":
		return true
	}
	return false
}

func isCancelCall(pass *analysis.Pass, d *ast.DeferStmt, cancel *types.Var) bool {
	if d.Call == nil || d.Call.Ellipsis.IsValid() {
		return false
	}
	id, ok := stripParens(d.Call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	return varOf(pass.TypesInfo, id) == cancel
}

func mentionsVar(pass *analysis.Pass, fn inspector.Cursor, v *types.Var) bool {
	for cur := range fn.Preorder((*ast.ReturnStmt)(nil)) {
		if mentionsAt(pass, fn, cur, v) {
			return true
		}
	}
	return false
}

func mentionsAt(pass *analysis.Pass, fn, cur inspector.Cursor, v *types.Var) bool {
	if ownerOf(cur) != fn {
		return false
	}
	ret, ok := cur.Node().(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, res := range ret.Results {
		if returnsContext(pass.TypesInfo, res, v) {
			return true
		}
	}
	return false
}

func returnsContext(info *types.Info, res ast.Expr, v *types.Var) bool {
	e := stripParens(res)
	if identResolvesTo(info, e, v) {
		return true
	}
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		return returnsContext(info, u.X, v)
	}
	if lit, ok := e.(*ast.CompositeLit); ok {
		return compositeMentions(info, lit, v)
	}
	if call, ok := e.(*ast.CallExpr); ok {
		if !callResultIsContext(info, call) {
			return false
		}
		for _, arg := range call.Args {
			if returnsContext(info, arg, v) {
				return true
			}
		}
	}
	return false
}

func callResultIsContext(info *types.Info, call *ast.CallExpr) bool {
	t := info.TypeOf(call)
	if t == nil {
		return false
	}
	if tup, ok := t.(*types.Tuple); ok {
		for v := range tup.Variables() {
			if implementsContext(v.Type()) {
				return true
			}
		}
		return false
	}
	return implementsContext(t)
}

func implementsContext(t types.Type) bool {
	if t == nil {
		return false
	}
	if isNamedContext(t) {
		return true
	}
	ms := types.NewMethodSet(t)
	return doneResultOK(ms) && errResultOK(ms) && valueResultOK(ms) && deadlineResultOK(ms)
}

func isNamedContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == contextName && obj.Pkg() != nil && obj.Pkg().Path() == contextPath
}

func doneResultOK(ms *types.MethodSet) bool {
	sel := ms.Lookup(nil, "Done")
	if sel == nil {
		return false
	}
	sig, ok := sel.Obj().Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	ch, ok := sig.Results().At(0).Type().(*types.Chan)
	return ok && ch.Dir() == types.RecvOnly
}

func errResultOK(ms *types.MethodSet) bool {
	sel := ms.Lookup(nil, "Err")
	if sel == nil {
		return false
	}
	sig, ok := sel.Obj().Type().(*types.Signature)
	return ok && sig.Params().Len() == 0 && sig.Results().Len() == 1
}

func valueResultOK(ms *types.MethodSet) bool {
	sel := ms.Lookup(nil, "Value")
	if sel == nil {
		return false
	}
	sig, ok := sel.Obj().Type().(*types.Signature)
	return ok && sig.Params().Len() == 1 && sig.Results().Len() == 1
}

func deadlineResultOK(ms *types.MethodSet) bool {
	sel := ms.Lookup(nil, "Deadline")
	if sel == nil {
		return false
	}
	sig, ok := sel.Obj().Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 2 {
		return false
	}
	basic, ok := sig.Results().At(1).Type().(*types.Basic)
	return ok && basic.Kind() == types.Bool
}

func compositeMentions(info *types.Info, lit *ast.CompositeLit, v *types.Var) bool {
	for _, elt := range lit.Elts {
		if compositeEltMentions(info, stripParens(elt), v) {
			return true
		}
	}
	return false
}

func compositeEltMentions(info *types.Info, elt ast.Expr, v *types.Var) bool {
	if kv, ok := elt.(*ast.KeyValueExpr); ok {
		return returnsContext(info, stripParens(kv.Value), v)
	}
	return returnsContext(info, elt, v)
}
