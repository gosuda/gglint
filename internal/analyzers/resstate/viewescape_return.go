package resstate

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func deferredPoolPut(pass *analysis.Pass, fn ast.Node, recvKey flow.Key) (ast.Expr, bool) {
	var pool ast.Expr
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if p, ok := poolPutArg(pass, d, recvKey); ok {
			pool = p
			found = true
			return false
		}
		return true
	})
	return pool, found
}

func poolPutArg(pass *analysis.Pass, d *ast.DeferStmt, recvKey flow.Key) (ast.Expr, bool) {
	call := d.Call
	if call == nil {
		return nil, false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != putMethod {
		return nil, false
	}
	if !isPoolType(pass.TypesInfo.Types[sel.X].Type) {
		return nil, false
	}
	for _, a := range call.Args {
		k, ok := flow.KeyOf(pass.TypesInfo, a)
		if ok && k == recvKey {
			return sel.X, true
		}
	}
	return nil, false
}
