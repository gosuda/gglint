package wgstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the four wgstate checks.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{addlateAnalyzer, doneskipAnalyzer, selfwaitAnalyzer, groupwaitAnalyzer}
}

func eachFunc(pass *analysis.Pass, f func(fn ast.Node)) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		f(cur.Node())
	}
}

func funcBody(fn ast.Node) *ast.BlockStmt {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		return fn.Body
	case *ast.FuncLit:
		return fn.Body
	}
	return nil
}

func eachList(fn ast.Node, visit func([]ast.Stmt)) {
	body := funcBody(fn)
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.BlockStmt:
			visit(n.List)
		case *ast.CaseClause:
			visit(n.Body)
		case *ast.CommClause:
			visit(n.Body)
		}
		return true
	})
}

func unwrap(s ast.Stmt) ast.Stmt {
	for {
		l, ok := s.(*ast.LabeledStmt)
		if !ok {
			return s
		}
		s = l.Stmt
	}
}

func directCall(s ast.Stmt) (*ast.CallExpr, bool) {
	e, ok := unwrap(s).(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(e.X).(*ast.CallExpr)
	return call, ok
}

func wgKey(info *types.Info, call *ast.CallExpr, name string) (flow.Key, string, bool) {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || fn.FullName() != "(*sync.WaitGroup)."+name {
		return flow.Key{}, "", false
	}
	k, ok := flow.MethodKey(info, call)
	if !ok {
		return flow.Key{}, "", false
	}
	return k, recvText(call), true
}

func recvText(call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return types.ExprString(sel.X)
}
