package spin

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var closedrecvAnalyzer = &analysis.Analyzer{
	Name:     "closedrecv",
	Doc:      "report single-value receives spinning after close; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runClosedrecv,
}

func runClosedrecv(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkClosedrecv(pass, cur, loop)
	})
	return nil, nil
}

func checkClosedrecv(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) {
	if loop.Cond != nil || loop.Init != nil || loop.Post != nil || loop.Body == nil {
		return
	}
	recv := loneRecv(loop.Body)
	if recv == nil {
		return
	}
	id, ok := ast.Unparen(recv.X).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	fn := enclosingFunc(cur)
	if !isLocalVar(fn, v) || !isChan(v.Type()) {
		return
	}
	if !closesBefore(pass, fn, v, loop.Pos()) {
		return
	}
	pass.Reportf(loop.Pos(), "after close, this receive from %s returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false", v.Name())
}

func loneRecv(body *ast.BlockStmt) *ast.UnaryExpr {
	var recv *ast.UnaryExpr
	ops := 0
	clean := true
	two := false
	walkNoLit(body, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CallExpr:
			clean = false
		case *ast.SelectStmt:
			clean = false
		case *ast.SendStmt:
			ops++
		case *ast.UnaryExpr:
			if t.Op == token.ARROW {
				ops++
				recv = t
			}
		case *ast.AssignStmt:
			if len(t.Lhs) == 2 && len(t.Rhs) == 1 && hasRecv(t.Rhs[0]) {
				two = true
			}
		case *ast.ValueSpec:
			if len(t.Names) == 2 && len(t.Values) == 1 && hasRecv(t.Values[0]) {
				two = true
			}
		}
		return clean && ops < 2 && !two
	})
	if !clean || ops != 1 || two {
		return nil
	}
	return recv
}

func hasRecv(e ast.Expr) bool {
	found := false
	walkNoLit(e, func(n ast.Node) bool {
		if recv, ok := n.(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
			found = true
		}
		return !found
	})
	return found
}

func isChan(t types.Type) bool {
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func closesBefore(pass *analysis.Pass, fn ast.Node, v *types.Var, pos token.Pos) bool {
	closed := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if closed {
			return false
		}
		gs, ok := n.(*ast.GoStmt)
		if !ok || gs.Pos() >= pos {
			return true
		}
		call, ok := ast.Unparen(gs.Call).(*ast.CallExpr)
		if !ok {
			return true
		}
		lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
		if !ok {
			return true
		}
		walkNoLit(lit.Body, func(x ast.Node) bool {
			c, ok := x.(*ast.CallExpr)
			if !ok || !pass.TypesInfo.Types[c.Fun].IsBuiltin() {
				return true
			}
			if len(c.Args) != 1 {
				return true
			}
			if id, ok := ast.Unparen(c.Args[0]).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == types.Object(v) {
				closed = true
			}
			return true
		})
		return true
	})
	return closed
}
