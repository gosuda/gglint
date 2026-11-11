package wgstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

var groupwaitAnalyzer = &analysis.Analyzer{
	Name:     "groupwait",
	Doc:      "report an errgroup Wait inside a function literal passed to Go or TryGo of the same group; a named function passed to Go is not checked, and a Wait in a nested function literal or go statement is not reported",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runGroupwait,
}

func runGroupwait(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkGroupwait(pass, fn)
	})
	return nil, nil
}

func checkGroupwait(pass *analysis.Pass, fn ast.Node) {
	body := funcBody(fn)
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			checkGroupCall(pass, call)
		}
		return true
	})
}

func checkGroupCall(pass *analysis.Pass, call *ast.CallExpr) {
	info := pass.TypesInfo
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return
	}
	switch fn.FullName() {
	case "(*golang.org/x/sync/errgroup.Group).Go", "(*golang.org/x/sync/errgroup.Group).TryGo":
	default:
		return
	}
	k, ok := flow.MethodKey(info, call)
	if !ok || len(call.Args) == 0 {
		return
	}
	lit, ok := ast.Unparen(call.Args[0]).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return
	}
	findGroupWaits(pass, lit, k)
}

func findGroupWaits(pass *analysis.Pass, lit *ast.FuncLit, k flow.Key) {
	info := pass.TypesInfo
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isGroupWait(info, call, k) {
			return true
		}
		pass.Reportf(call.Pos(), "this Wait runs inside a function of the same group, so it waits for itself and never returns: move the Wait to the goroutine that started the group, and return this function's error instead")
		return true
	})
}

func isGroupWait(info *types.Info, call *ast.CallExpr, k flow.Key) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || fn.FullName() != "(*golang.org/x/sync/errgroup.Group).Wait" {
		return false
	}
	wk, ok := flow.MethodKey(info, call)
	return ok && wk == k
}
