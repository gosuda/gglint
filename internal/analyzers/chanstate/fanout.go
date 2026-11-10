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
	if !found {
		return chanOp{}, false
	}
	for _, s := range lit.Body.List {
		if s == send.Expr {
			return send, true
		}
	}
	return chanOp{}, false
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

func unwrapLabeled(s ast.Stmt) ast.Stmt {
	for {
		l, ok := s.(*ast.LabeledStmt)
		if !ok {
			return s
		}
		s = l.Stmt
	}
}

func indexOfLoop(l []ast.Stmt, target ast.Stmt) int {
	for i, c := range l {
		if c == target {
			return i
		}
		if unwrapLabeled(c) == target {
			return i
		}
	}
	return -1
}

func straightRecvs(info *types.Info, fn ast.Node, loop ast.Stmt, k flow.Key) (int64, bool) {
	list := enclosingList(fn, loop)
	if list == nil {
		return 0, false
	}
	start := indexOfLoop(list, loop)
	if start < 0 {
		return 0, false
	}
	rep := list[start]
	var count int64
	for _, s := range list[start+1:] {
		stop, fail := stepAfterLoop(info, fn, rep, s, k, &count)
		if fail {
			return 0, false
		}
		if stop {
			break
		}
	}
	return count, true
}

func stepAfterLoop(info *types.Info, fn ast.Node, loop, s ast.Stmt, k flow.Key, count *int64) (stop, failed bool) {
	if _, ok := s.(*ast.ReturnStmt); ok {
		if flow.StraightLine(fn, loop, s) {
			*count += recvCount(info, s, k)
		}
		return true, false
	}
	if !isStraightStmt(s) {
		add, ok := drainLoopRecvs(info, s, k)
		if !ok {
			return false, true
		}
		*count += add
		return false, false
	}
	if !flow.StraightLine(fn, loop, s) {
		return false, false
	}
	*count += recvCount(info, s, k)
	return false, false
}

func drainLoopRecvs(info *types.Info, s ast.Stmt, k flow.Key) (int64, bool) {
	switch s := s.(type) {
	case *ast.ForStmt, *ast.RangeStmt:
		return drainCountedLoop(info, s, k)
	case *ast.IfStmt:
		return drainIfRecvs(info, s, k)
	case *ast.SwitchStmt:
		return stmtRecvs(info, s.Init, k) + uncondRecvs(info, s.Tag, k), true
	}
	return 0, true
}

func drainCountedLoop(info *types.Info, s ast.Stmt, k flow.Key) (int64, bool) {
	straight := drainBodyStraight(info, s, k)
	if countLoopRecvs(info, s, k) != straight {
		return 0, false
	}
	if straight == 0 {
		return 0, true
	}
	trip, ok := loopTrip(info, s)
	if !ok {
		return 0, false
	}
	return trip * straight, true
}

func drainIfRecvs(info *types.Info, s *ast.IfStmt, k flow.Key) (int64, bool) {
	header := stmtRecvs(info, s.Init, k) + uncondRecvs(info, s.Cond, k)
	if s.Else == nil {
		return header, true
	}
	bodyCount, ok := blockStraightRecvs(info, s.Body, k)
	if !ok {
		return 0, false
	}
	elseCount, ok := drainElseRecvs(info, s.Else, k)
	if !ok {
		return 0, false
	}
	if bodyCount < elseCount {
		return header + bodyCount, true
	}
	return header + elseCount, true
}

func drainElseRecvs(info *types.Info, elseStmt ast.Stmt, k flow.Key) (int64, bool) {
	if body, ok := elseStmt.(*ast.BlockStmt); ok {
		return blockStraightRecvs(info, body, k)
	}
	if s, ok := elseStmt.(*ast.IfStmt); ok {
		return drainIfRecvs(info, s, k)
	}
	return 0, false
}

func blockStraightRecvs(info *types.Info, body *ast.BlockStmt, k flow.Key) (int64, bool) {
	if body == nil {
		return 0, true
	}
	var n int64
	for _, st := range body.List {
		if !isStraightStmt(st) {
			return 0, false
		}
		n += recvCount(info, st, k)
	}
	return n, true
}

func uncondRecvs(info *types.Info, x ast.Expr, k flow.Key) int64 {
	if x == nil {
		return 0
	}
	return countUncond(info, ast.Unparen(x), k)
}

func countUncond(info *types.Info, x ast.Expr, k flow.Key) int64 {
	switch x := x.(type) {
	case nil:
		return 0
	case *ast.BinaryExpr:
		return countUncondBinary(info, x, k)
	case *ast.UnaryExpr:
		return countUncondUnary(info, x, k)
	case *ast.CallExpr:
		return countUncondCall(info, x, k)
	default:
		return countUncondCompound(info, x, k)
	}
}

func countUncondBinary(info *types.Info, x *ast.BinaryExpr, k flow.Key) int64 {
	if x.Op == token.LAND || x.Op == token.LOR {
		return countUncond(info, ast.Unparen(x.X), k)
	}
	return countUncond(info, ast.Unparen(x.X), k) + countUncond(info, ast.Unparen(x.Y), k)
}

func countUncondUnary(info *types.Info, x *ast.UnaryExpr, k flow.Key) int64 {
	if x.Op == token.ARROW {
		if kk, ok := flow.KeyOf(info, x.X); ok && kk == k {
			return 1
		}
		return 0
	}
	return countUncond(info, ast.Unparen(x.X), k)
}

func countUncondCall(info *types.Info, x *ast.CallExpr, k flow.Key) int64 {
	n := countUncond(info, ast.Unparen(x.Fun), k)
	for _, a := range x.Args {
		n += countUncond(info, ast.Unparen(a), k)
	}
	return n
}

func countUncondCompound(info *types.Info, x ast.Expr, k flow.Key) int64 {
	switch x := x.(type) {
	case *ast.StarExpr:
		return countUncond(info, ast.Unparen(x.X), k)
	case *ast.SelectorExpr:
		return countUncond(info, ast.Unparen(x.X), k)
	case *ast.TypeAssertExpr:
		return countUncond(info, ast.Unparen(x.X), k)
	case *ast.Ellipsis:
		if x.Elt == nil {
			return 0
		}
		return countUncond(info, ast.Unparen(x.Elt), k)
	case *ast.FuncLit, *ast.Ident, *ast.BasicLit:
		return 0
	default:
		return countUncondIndex(info, x, k)
	}
}

func countUncondIndex(info *types.Info, x ast.Expr, k flow.Key) int64 {
	switch x := x.(type) {
	case *ast.IndexExpr:
		return countUncond(info, ast.Unparen(x.X), k) + countUncond(info, ast.Unparen(x.Index), k)
	case *ast.IndexListExpr:
		n := countUncond(info, ast.Unparen(x.X), k)
		for _, ix := range x.Indices {
			n += countUncond(info, ast.Unparen(ix), k)
		}
		return n
	case *ast.SliceExpr:
		return countUncondSlice(info, x, k)
	case *ast.CompositeLit:
		var n int64
		for _, e := range x.Elts {
			n += countUncond(info, ast.Unparen(e), k)
		}
		return n
	case *ast.KeyValueExpr:
		return countUncond(info, ast.Unparen(x.Key), k) + countUncond(info, ast.Unparen(x.Value), k)
	case *ast.ParenExpr:
		return countUncond(info, ast.Unparen(x.X), k)
	}
	return exprRecvs(info, x, k)
}

func countUncondSlice(info *types.Info, x *ast.SliceExpr, k flow.Key) int64 {
	n := countUncond(info, ast.Unparen(x.X), k)
	for _, bound := range []ast.Expr{x.Low, x.High, x.Max} {
		if bound != nil {
			n += countUncond(info, ast.Unparen(bound), k)
		}
	}
	return n
}

func stmtRecvs(info *types.Info, s ast.Stmt, k flow.Key) int64 {
	if s == nil {
		return 0
	}
	return recvCount(info, s, k)
}

func exprRecvs(info *types.Info, x ast.Expr, k flow.Key) int64 {
	if x == nil {
		return 0
	}
	w := &opWalker{info: info, key: k}
	w.expr(x)
	var count int64
	for _, op := range w.out {
		if op.Kind == opRecv && !op.InSelect {
			count++
		}
	}
	return count
}

func drainBodyStraight(info *types.Info, s ast.Stmt, k flow.Key) int64 {
	var count int64
	for _, b := range loopBody(s) {
		if !isStraightStmt(b) {
			continue
		}
		count += recvCount(info, b, k)
	}
	return count
}

func countLoopRecvs(info *types.Info, s ast.Stmt, k flow.Key) int64 {
	w := &opWalker{info: info, key: k}
	w.stmt(s)
	var count int64
	for _, op := range w.out {
		if op.Kind == opRecv && !op.InSelect {
			count++
		}
	}
	return count
}

func enclosingList(fn ast.Node, target ast.Stmt) []ast.Stmt {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	if indexOfLoop(body.List, target) >= 0 {
		return body.List
	}
	return searchEnclosing(body, target)
}

func searchEnclosing(body *ast.BlockStmt, target ast.Stmt) []ast.Stmt {
	var found []ast.Stmt
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if list := listForNode(n, target); list != nil {
			found = list
		}
		return found == nil
	})
	return found
}

func listForNode(n ast.Node, target ast.Stmt) []ast.Stmt {
	switch n := n.(type) {
	case *ast.BlockStmt:
		return listContaining(n.List, target)
	case *ast.CaseClause:
		return listContaining(n.Body, target)
	case *ast.CommClause:
		return listContaining(n.Body, target)
	}
	return nil
}

func listContaining(list []ast.Stmt, target ast.Stmt) []ast.Stmt {
	if indexOfLoop(list, target) >= 0 {
		return list
	}
	return nil
}

func isStraightStmt(s ast.Stmt) bool {
	switch s.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.GoStmt, *ast.DeferStmt, *ast.BranchStmt, *ast.LabeledStmt:
		return false
	}
	return true
}

func recvCount(info *types.Info, s ast.Stmt, k flow.Key) int64 {
	w := &opWalker{info: info, key: k}
	w.stmt(s)
	var count int64
	for _, op := range w.out {
		if op.Kind == opRecv && !op.InSelect {
			count++
		}
	}
	return count
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

func loopHasRecv(info *types.Info, loop ast.Stmt, k flow.Key) bool {
	w := &opWalker{info: info, key: k}
	w.stmts(loopBody(loop))
	for _, op := range w.out {
		if op.Kind == opRecv {
			return true
		}
	}
	return false
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

func underSelect(fn, target ast.Node) bool {
	found := false
	ast.Inspect(fn, func(x ast.Node) bool {
		if found {
			return false
		}
		sel, ok := x.(*ast.SelectStmt)
		if !ok {
			return true
		}
		for _, c := range sel.Body.List {
			cc, ok := c.(*ast.CommClause)
			if !ok || cc.Comm == nil {
				continue
			}
			if withinNode(cc.Comm, target) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func withinNode(outer, inner ast.Node) bool {
	return outer.Pos() <= inner.Pos() && inner.End() <= outer.End()
}
