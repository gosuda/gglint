package reentrantlock

import (
	"go/ast"
	"go/token"
	"go/types"
)

type returnPaths struct {
	next     bool
	returned bool
}

// Start with no returning helpers, so a call cycle needs a reachable base
// return before its callers can export releases after the call.
func (c *checker) blockNonReturning() {
	returning := make(map[*types.Func]bool)
	for grew := true; grew; {
		grew = false
		for _, fn := range c.order {
			if c.releaseBarrier[fn] || returning[fn] {
				continue
			}
			paths := c.returningStatements(c.decls[fn].Body.List, returning)
			if paths.next || paths.returned {
				returning[fn] = true
				grew = true
			}
		}
	}
	for _, fn := range c.order {
		c.releaseBarrier[fn] = c.releaseBarrier[fn] || !returning[fn]
	}
}

func (c *checker) returningStatements(list []ast.Stmt, returning map[*types.Func]bool) returnPaths {
	paths := returnPaths{next: true}
	for _, stmt := range list {
		if !paths.next {
			break
		}
		next := c.returningStatement(stmt, returning)
		paths.next = next.next
		paths.returned = paths.returned || next.returned
	}
	return paths
}

func (c *checker) returningStatement(stmt ast.Stmt, returning map[*types.Func]bool) returnPaths {
	switch stmt := stmt.(type) {
	case *ast.BlockStmt:
		return c.returningStatements(stmt.List, returning)
	case *ast.ExprStmt:
		return returnPaths{next: c.returningExpression(stmt.X, returning)}
	case *ast.ReturnStmt:
		return returnPaths{returned: c.returningExpressions(stmt.Results, returning)}
	case *ast.IfStmt:
		return c.returningIf(stmt, returning)
	case *ast.DeferStmt:
		return returnPaths{next: c.returningCall(stmt.Call, returning)}
	case *ast.LabeledStmt:
		return c.returningStatement(stmt.Stmt, returning)
	case *ast.SendStmt:
		return returnPaths{next: c.returningExpressions([]ast.Expr{stmt.Chan, stmt.Value}, returning)}
	case *ast.DeclStmt, *ast.EmptyStmt, nil:
		return returnPaths{next: true}
	default:
		return returnPaths{}
	}
}

func (c *checker) returningIf(stmt *ast.IfStmt, returning map[*types.Func]bool) returnPaths {
	if !c.returningStatement(stmt.Init, returning).next || !c.returningExpression(stmt.Cond, returning) {
		return returnPaths{}
	}
	if value, known := c.boolean(stmt.Cond); known {
		if value {
			return c.returningStatement(stmt.Body, returning)
		}
		return c.returningStatement(stmt.Else, returning)
	}
	then := c.returningStatement(stmt.Body, returning)
	other := c.returningStatement(stmt.Else, returning)
	return returnPaths{next: then.next || other.next, returned: then.returned || other.returned}
}

func (c *checker) returningExpressions(expressions []ast.Expr, returning map[*types.Func]bool) bool {
	for _, expr := range expressions {
		if !c.returningExpression(expr, returning) {
			return false
		}
	}
	return true
}

func (c *checker) returningExpression(expr ast.Expr, returning map[*types.Func]bool) bool {
	result := true
	ast.Inspect(expr, func(node ast.Node) bool {
		if !result {
			return false
		}
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			result = c.returningCall(node, returning)
			return false
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				result = c.returningShortCircuit(node, returning)
				return false
			}
		}
		return true
	})
	return result
}

func (c *checker) returningShortCircuit(expr *ast.BinaryExpr, returning map[*types.Func]bool) bool {
	if !c.returningExpression(expr.X, returning) {
		return false
	}
	if value, known := c.boolean(expr.X); known && value == (expr.Op == token.LAND) {
		return c.returningExpression(expr.Y, returning)
	}
	return true
}

func (c *checker) returningCall(call *ast.CallExpr, returning map[*types.Func]bool) bool {
	if c.pass.TypesInfo.Types[call].Value != nil {
		return true
	}
	if !c.returningExpression(call.Fun, returning) || !c.returningExpressions(call.Args, returning) {
		return false
	}
	if literal, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		paths := c.returningStatements(literal.Body.List, returning)
		return paths.next || paths.returned
	}
	fn := c.callee(call)
	return fn == nil || returning[fn]
}
