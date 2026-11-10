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
	Doc:      "report a bare loop receiving once per pass from a local channel closed by a spawned literal before the loop when the loop has no exit and the channel is never reassigned; spin covers five shapes only",
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
	if !bareLoop(loop) {
		return
	}
	recv := loneRecv(pass.TypesInfo, loop.Body)
	if recv == nil {
		return
	}
	v, ok := recvVar(pass, recv)
	if !ok {
		return
	}
	fn := enclosingFunc(cur)
	if !isLocalVar(fn, v) || !isChan(v.Type()) {
		return
	}
	if channelReassigned(pass.TypesInfo, fn, v) {
		return
	}
	if !closesBefore(pass, fn, loop, v) {
		return
	}
	if loopExits(pass.TypesInfo, cur, loop) {
		return
	}
	pass.Reportf(loop.Pos(), "after close, this receive from %s returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false", v.Name())
}

func bareLoop(loop *ast.ForStmt) bool {
	return loop.Cond == nil && loop.Body != nil
}

func channelReassigned(info *types.Info, fn ast.Node, v *types.Var) bool {
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if reassignsVar(info, n, v) {
			found = true
		}
		return !found
	})
	return found
}

func reassignsVar(info *types.Info, n ast.Node, v *types.Var) bool {
	switch t := n.(type) {
	case *ast.AssignStmt:
		return t.Tok == token.ASSIGN && t.Pos() > v.Pos() && lhsHits(info, t.Lhs, v)
	case *ast.RangeStmt:
		return t.Tok == token.ASSIGN && t.Pos() > v.Pos() && rangeHits(info, t, v)
	default:
		return false
	}
}

func recvVar(pass *analysis.Pass, recv *ast.UnaryExpr) (*types.Var, bool) {
	id, ok := ast.Unparen(recv.X).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	return v, true
}

func loneRecv(info *types.Info, body *ast.BlockStmt) *ast.UnaryExpr {
	s := &loneScan{clean: true, info: info}
	walkNoLit(body, s.visit)
	if !s.clean || s.ops != 1 || s.two {
		return nil
	}
	return s.recv
}

type loneScan struct {
	recv  *ast.UnaryExpr
	ops   int
	clean bool
	two   bool
	info  *types.Info
}

func (s *loneScan) visit(n ast.Node) bool {
	switch t := n.(type) {
	case *ast.CallExpr:
		s.markCall(t)
	case *ast.SelectStmt:
		s.clean = false
	case *ast.SendStmt:
		s.ops++
	case *ast.UnaryExpr:
		s.addRecv(t)
	case *ast.AssignStmt:
		s.addAssign(t)
	case *ast.ValueSpec:
		s.addValue(t)
	}
	return s.clean && s.ops < 2 && !s.two
}

func (s *loneScan) markCall(t *ast.CallExpr) {
	if isCodeCall(s.info, t) {
		s.clean = false
	}
}

func (s *loneScan) addRecv(t *ast.UnaryExpr) {
	if t.Op != token.ARROW {
		return
	}
	s.ops++
	s.recv = t
}

func (s *loneScan) addAssign(t *ast.AssignStmt) {
	if twoExprs(t.Lhs, t.Rhs) {
		s.two = true
	}
}

func (s *loneScan) addValue(t *ast.ValueSpec) {
	if len(t.Names) == 2 && len(t.Values) == 1 && hasRecv(t.Values[0]) {
		s.two = true
	}
}

func twoExprs(lhs, rhs []ast.Expr) bool {
	if len(lhs) != 2 || len(rhs) != 1 {
		return false
	}
	return hasRecv(rhs[0])
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

func calledLits(n ast.Node) map[*ast.FuncLit]bool {
	lits := map[*ast.FuncLit]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		c, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if lit, ok := ast.Unparen(c.Fun).(*ast.FuncLit); ok {
			lits[lit] = true
		}
		return true
	})
	return lits
}

func walkCalledLit(n ast.Node, f func(ast.Node) bool) {
	lits := calledLits(n)
	ast.Inspect(n, func(x ast.Node) bool {
		if x == nil {
			return true
		}
		if lit, ok := x.(*ast.FuncLit); ok && x != n && !lits[lit] {
			return false
		}
		return f(x)
	})
}

func closesBefore(pass *analysis.Pass, fn ast.Node, loop *ast.ForStmt, v *types.Var) bool {
	closed := false
	walkCalledLit(fn, func(n ast.Node) bool {
		if closed {
			return false
		}
		if n == nil || n.Pos() >= loop.Pos() {
			return n == nil
		}
		lit, ok := spawnedBody(n)
		if !ok {
			return true
		}
		if litCloses(pass, lit, v) {
			closed = true
		}
		return true
	})
	return closed
}

func spawnedBody(n ast.Node) (*ast.FuncLit, bool) {
	gs, ok := n.(*ast.GoStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(gs.Call).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
	if !ok {
		return nil, false
	}
	return lit, true
}

func litCloses(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) bool {
	closed := false
	walkCalledLit(lit.Body, func(x ast.Node) bool {
		if closesVar(pass, x, v) {
			closed = true
		}
		return !closed
	})
	return closed
}

func closesVar(pass *analysis.Pass, x ast.Node, v *types.Var) bool {
	c, ok := x.(*ast.CallExpr)
	if !ok || len(c.Args) != 1 {
		return false
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := pass.TypesInfo.ObjectOf(id).(*types.Builtin)
	if !ok || b.Name() != "close" {
		return false
	}
	arg, ok := ast.Unparen(c.Args[0]).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(arg) == types.Object(v)
}

type exitQuery struct {
	info  *types.Info
	label string
	inner map[string]bool
}

func loopExits(info *types.Info, cur inspector.Cursor, loop *ast.ForStmt) bool {
	if loop.Body == nil {
		return false
	}
	q := exitQuery{info: info, label: loopLabel(cur), inner: innerLabelSet(loop.Body)}
	return stmtsExit(q, loop.Body.List, 0)
}

func innerLabelSet(body *ast.BlockStmt) map[string]bool {
	labels := map[string]bool{}
	walkNoLit(body, func(n ast.Node) bool {
		if ls, ok := n.(*ast.LabeledStmt); ok {
			labels[ls.Label.Name] = true
		}
		return true
	})
	return labels
}

func stmtsExit(q exitQuery, list []ast.Stmt, depth int) bool {
	for _, stmt := range list {
		if stmtExits(q, stmt, depth) {
			return true
		}
	}
	return false
}

func stmtExits(q exitQuery, stmt ast.Stmt, depth int) bool {
	switch t := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return branchExits(q, t, depth)
	case *ast.ExprStmt:
		return isPanicCall(q.info, t.X)
	case *ast.BlockStmt:
		return stmtsExit(q, t.List, depth)
	case *ast.IfStmt:
		return ifExits(q, t, depth)
	case *ast.LabeledStmt:
		return stmtExits(q, t.Stmt, depth)
	default:
		return nestedExits(q, stmt, depth)
	}
}

func branchExits(q exitQuery, b *ast.BranchStmt, depth int) bool {
	if b.Tok == token.GOTO {
		return true
	}
	if b.Tok == token.BREAK {
		if b.Label == nil {
			return depth == 0
		}
		if b.Label.Name == q.label {
			return true
		}
		return !q.inner[b.Label.Name]
	}
	if b.Tok == token.CONTINUE {
		if b.Label == nil || b.Label.Name == q.label {
			return false
		}
		return !q.inner[b.Label.Name]
	}
	return false
}

func ifExits(q exitQuery, t *ast.IfStmt, depth int) bool {
	if t.Body != nil && stmtsExit(q, t.Body.List, depth) {
		return true
	}
	switch e := t.Else.(type) {
	case *ast.BlockStmt:
		return stmtsExit(q, e.List, depth)
	case *ast.IfStmt:
		return ifExits(q, e, depth)
	default:
		return false
	}
}

func nestedExits(q exitQuery, stmt ast.Stmt, depth int) bool {
	switch t := stmt.(type) {
	case *ast.ForStmt:
		return loopBodyExits(q, t.Body, depth)
	case *ast.RangeStmt:
		return loopBodyExits(q, t.Body, depth)
	case *ast.SelectStmt:
		return selectExits(q, t, depth)
	case *ast.SwitchStmt:
		return casesExit(q, t.Body, depth)
	case *ast.TypeSwitchStmt:
		return casesExit(q, t.Body, depth)
	default:
		return false
	}
}

func loopBodyExits(q exitQuery, body *ast.BlockStmt, depth int) bool {
	if body == nil {
		return false
	}
	return stmtsExit(q, body.List, depth+1)
}

func selectExits(q exitQuery, t *ast.SelectStmt, depth int) bool {
	if t.Body == nil {
		return false
	}
	for _, s := range t.Body.List {
		if c, ok := s.(*ast.CommClause); ok && stmtsExit(q, c.Body, depth+1) {
			return true
		}
	}
	return false
}

func casesExit(q exitQuery, body *ast.BlockStmt, depth int) bool {
	if body == nil {
		return false
	}
	for _, stmt := range body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if stmtsExit(q, clause.Body, depth+1) {
			return true
		}
	}
	return false
}
