package chanstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gosuda/gglint/internal/flow"
)

func loneSend(info *types.Info, lit *ast.FuncLit) (chanOp, bool) {
	if lit == nil {
		return chanOp{}, false
	}
	send, ok := onlySend(info, lit)
	if !ok {
		return chanOp{}, false
	}
	for _, s := range lit.Body.List {
		if s != send.Expr {
			continue
		}
		if exitsBeforeSend(info, lit, send.Pos) {
			return chanOp{}, false
		}
		return send, true
	}
	return chanOp{}, false
}

func onlySend(info *types.Info, lit *ast.FuncLit) (chanOp, bool) {
	w := &opWalker{info: info}
	w.stmts(lit.Body.List)
	var send chanOp
	found := false
	for _, op := range w.out {
		if op.Kind != opSend || op.InSelect {
			continue
		}
		if found {
			return chanOp{}, false
		}
		send, found = op, true
	}
	return send, found
}

func exitsBeforeSend(info *types.Info, lit *ast.FuncLit, before token.Pos) bool {
	exited := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if exited {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		exited = exitsAt(info, n, before)
		return !exited
	})
	return exited
}

func exitsAt(info *types.Info, n ast.Node, before token.Pos) bool {
	switch n := n.(type) {
	case *ast.ReturnStmt:
		return n.Pos() < before
	case *ast.BranchStmt:
		return n.Pos() < before && n.Tok == token.GOTO
	case *ast.CallExpr:
		return n.Pos() < before && isPanicCall(info, n)
	}
	return false
}

func isPanicCall(info *types.Info, c *ast.CallExpr) bool {
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok || id.Name != "panic" {
		return false
	}
	return info.ObjectOf(id) == types.Universe.Lookup("panic")
}

func loneSends(info *types.Info, lit *ast.FuncLit) []chanOp {
	if lit == nil {
		return nil
	}
	w := &opWalker{info: info}
	w.stmts(lit.Body.List)
	var out []chanOp
	for _, op := range w.out {
		if op.Kind != opSend || op.InSelect {
			continue
		}
		out = append(out, op)
	}
	return out
}

func loneSelectRecv(info *types.Info, fn ast.Node, k flow.Key) (*ast.SelectStmt, chanOp, bool) {
	body := flow.Body(fn)
	if body == nil {
		return nil, chanOp{}, false
	}
	w := &opWalker{info: info, key: k}
	w.stmts(body.List)
	var recv chanOp
	found := false
	for _, op := range w.out {
		if op.Kind != opRecv {
			continue
		}
		if found {
			return nil, chanOp{}, false
		}
		recv, found = op, true
	}
	if !found || recv.Sel == nil {
		return nil, chanOp{}, false
	}
	return recv.Sel, recv, true
}

func otherReturns(sel *ast.SelectStmt, recv ast.Node) bool {
	for _, s := range sel.Body.List {
		cc, ok := s.(*ast.CommClause)
		if !ok || insideComm(cc, recv) {
			continue
		}
		if flow.EndsInReturn(cc.Body) {
			return true
		}
	}
	return false
}

func insideComm(cc *ast.CommClause, n ast.Node) bool {
	return cc.Comm != nil && cc.Comm.Pos() <= n.Pos() && n.End() <= cc.Comm.End()
}

func countLoops(fn ast.Node) []ast.Stmt {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []ast.Stmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.ForStmt:
			out = append(out, n)
		case *ast.RangeStmt:
			out = append(out, n)
		}
		return true
	})
	return out
}

func directGos(l []ast.Stmt) []*ast.GoStmt {
	var out []*ast.GoStmt
	for _, s := range l {
		if g, ok := s.(*ast.GoStmt); ok {
			out = append(out, g)
		}
	}
	return out
}

func loopTrip(info *types.Info, st ast.Stmt) (int64, bool) {
	switch st := st.(type) {
	case *ast.ForStmt:
		return forTrip(info, st)
	case *ast.RangeStmt:
		if hasBranchOrReturn(st.Body) {
			return 0, false
		}
		return constInt(info, st.X)
	}
	return 0, false
}

func forTrip(info *types.Info, st *ast.ForStmt) (int64, bool) {
	id, ok := forTripID(info, st)
	if !ok {
		return 0, false
	}
	return forTripBound(info, st, id)
}

func forTripID(info *types.Info, st *ast.ForStmt) (*ast.Ident, bool) {
	def, ok := st.Init.(*ast.AssignStmt)
	if !ok || def.Tok != token.DEFINE || len(def.Lhs) != 1 || len(def.Rhs) != 1 {
		return nil, false
	}
	id, ok := def.Lhs[0].(*ast.Ident)
	if !ok || !zeroInit(info, def.Rhs[0]) {
		return nil, false
	}
	if !incPost(info, st.Post, id) {
		return nil, false
	}
	return id, true
}

func forTripBound(info *types.Info, st *ast.ForStmt, id *ast.Ident) (int64, bool) {
	cond, ok := st.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LSS {
		return 0, false
	}
	if !isLoopVar(info, cond.X, id) {
		return 0, false
	}
	n, ok := constInt(info, cond.Y)
	if !ok || st.Body == nil {
		return 0, false
	}
	if hasBranchOrReturn(st.Body) {
		return 0, false
	}
	if assignsVar(info, st.Body, id) {
		return 0, false
	}
	return n, true
}

func isLoopVar(info *types.Info, e ast.Expr, id *ast.Ident) bool {
	u, ok := ast.Unparen(e).(*ast.Ident)
	return ok && info.Uses[u] == info.Defs[id]
}

func hasBranchOrReturn(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n.(type) {
		case *ast.BranchStmt, *ast.ReturnStmt:
			found = true
		}
		return !found
	})
	return found
}

func assignsVar(info *types.Info, body *ast.BlockStmt, id *ast.Ident) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		found = assignsInNode(info, n, id)
		return !found
	})
	return found || addrTaken(info, body, id)
}

func assignsInNode(info *types.Info, n ast.Node, id *ast.Ident) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return assignsInLHS(info, n.Lhs, id)
	case *ast.IncDecStmt:
		return loopsIdent(info, n.X, id)
	}
	return false
}

func assignsInLHS(info *types.Info, lhs []ast.Expr, id *ast.Ident) bool {
	for _, l := range lhs {
		if loopsIdent(info, l, id) {
			return true
		}
	}
	return false
}

func addrTaken(info *types.Info, body *ast.BlockStmt, id *ast.Ident) bool {
	taken := false
	ast.Inspect(body, func(n ast.Node) bool {
		if taken {
			return false
		}
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND && loopsIdent(info, u.X, id) {
			taken = true
		}
		return !taken
	})
	return taken
}

func zeroInit(info *types.Info, e ast.Expr) bool {
	n, ok := constInt(info, e)
	return ok && n == 0
}

func incPost(info *types.Info, post ast.Stmt, id *ast.Ident) bool {
	inc, ok := post.(*ast.IncDecStmt)
	if !ok || inc.Tok != token.INC {
		return false
	}
	return loopsIdent(info, inc.X, id)
}

func loopsIdent(info *types.Info, n ast.Node, id *ast.Ident) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if u, ok := x.(*ast.Ident); ok && info.Uses[u] == info.Defs[id] {
			found = true
		}
		return !found
	})
	return found
}

func withinNode(outer, inner ast.Node) bool {
	return outer.Pos() <= inner.Pos() && inner.End() <= outer.End()
}
