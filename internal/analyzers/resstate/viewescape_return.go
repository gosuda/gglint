package resstate

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func returnKept(pass *analysis.Pass, fn ast.Node, sink viewSink, recvKey flow.Key, viewMethod string) bool {
	v, ok := recvKey.Root.(*types.Var)
	if recvKey.Path != "" || !ok || !flow.IsLocal(pass.TypesInfo, fn, v) {
		return false
	}
	if deferForcesReport(pass, fn, v, recvKey, viewMethod) {
		return false
	}
	if escapesElsewhere(pass, fn, sink, v) {
		return false
	}
	return true
}

func deferForcesReport(pass *analysis.Pass, fn ast.Node, v *types.Var, recvKey flow.Key, viewMethod string) bool {
	forced := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if forced {
			return false
		}
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if deferInvalidates(pass, d, recvKey, viewMethod) {
			forced = true
		}
		if escapesVar(pass, fn, d, v) {
			forced = true
		}
		return !forced
	})
	return forced
}

func deferInvalidates(pass *analysis.Pass, d *ast.DeferStmt, recvKey flow.Key, viewMethod string) bool {
	bad := false
	ast.Inspect(d, func(n ast.Node) bool {
		if bad {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if invalidCallMatch(pass, call, recvKey, viewMethod) {
			bad = true
		}
		return !bad
	})
	return bad
}

func escapesElsewhere(pass *analysis.Pass, fn ast.Node, sink viewSink, v *types.Var) bool {
	sinkRet := containingReturn(fn, sink.expr)
	escaped := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if escaped {
			return false
		}
		if n == nil {
			return false
		}
		if lit, isLit := n.(*ast.FuncLit); isLit && lit != fn {
			return false
		}
		if n == sinkRet {
			return true
		}
		if escapesVar(pass, fn, n, v) {
			escaped = true
		}
		return !escaped
	})
	return escaped
}

func containingReturn(fn ast.Node, expr ast.Expr) *ast.ReturnStmt {
	var out *ast.ReturnStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		if lit, isLit := n.(*ast.FuncLit); isLit && lit != fn {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if slices.Contains(ret.Results, expr) {
			out = ret
			return false
		}
		return true
	})
	return out
}
