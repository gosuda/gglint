package reentrantlock

import "go/ast"

type flow struct {
	c       *checker
	onCall  func(*ast.CallExpr)
	onStmts func([]ast.Stmt)
}

func (f *flow) walk(n ast.Node) {
	ast.Inspect(n, f.visit)
}

func (f *flow) walkStmts(list []ast.Stmt) {
	for _, s := range list {
		f.walk(s)
	}
}

func (f *flow) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.GoStmt:
		f.async(n.Call)
		return false
	case *ast.FuncLit:
		return false
	case *ast.CallExpr:
		f.call(n)
		return false
	case *ast.BlockStmt:
		f.stmts(n.List)
	case *ast.CaseClause:
		f.stmts(n.Body)
	case *ast.CommClause:
		f.stmts(n.Body)
	}
	return true
}

func (f *flow) stmts(list []ast.Stmt) {
	if f.onStmts != nil {
		f.onStmts(list)
	}
}

func (f *flow) call(call *ast.CallExpr) {
	if f.onCall != nil {
		f.onCall(call)
	}
	f.operand(call.Fun)
	invoked := f.c.invokes[f.c.callee(call)]
	for i, arg := range call.Args {
		if invoked[i] {
			f.operand(arg)
		} else {
			f.walk(arg)
		}
	}
}

func (f *flow) operand(e ast.Expr) {
	if lit, ok := ast.Unparen(e).(*ast.FuncLit); ok {
		f.walk(lit.Body)
		return
	}
	f.walk(e)
}

func (f *flow) async(call *ast.CallExpr) {
	if _, ok := ast.Unparen(call.Fun).(*ast.FuncLit); !ok {
		f.walk(call.Fun)
	}
	f.walkExprs(call.Args)
}

func (f *flow) walkExprs(list []ast.Expr) {
	for _, e := range list {
		f.walk(e)
	}
}
