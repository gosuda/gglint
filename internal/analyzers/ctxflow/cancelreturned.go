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
		if resultMentions(pass.TypesInfo, res, v) {
			return true
		}
	}
	return false
}

func resultMentions(info *types.Info, res ast.Expr, v *types.Var) bool {
	found := false
	ast.Inspect(res, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && varOf(info, id) == v {
			found = true
			return false
		}
		return !found
	})
	return found
}
