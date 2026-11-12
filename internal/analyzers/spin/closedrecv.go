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
	Doc:      "report a bare loop receiving once per pass from a local channel closed by a spawned literal before the loop when the loop has no exit and the channel is never reassigned; a wait in the post statement does not end the loop, so that loop is reported too; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runClosedrecv,
}

func runClosedrecv(pass *analysis.Pass) (any, error) {
	cache := map[ast.Node]*fnCloses{}
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkClosedrecv(pass, cur, loop, cache)
	})
	return nil, nil
}

type fnCloses struct {
	reassigned map[*types.Var]bool
	firstClose map[*types.Var]token.Pos
}

func checkClosedrecv(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt, cache map[ast.Node]*fnCloses) {
	v, fn, ok := closedrecvCandidate(pass, cur, loop)
	if !ok {
		return
	}
	if loopExits(pass.TypesInfo, cur, loop) {
		return
	}
	fc := closesFor(pass, fn, cache)
	if fc.reassigned[v] {
		return
	}
	pos, ok := fc.firstClose[v]
	if !ok || pos >= loop.Pos() {
		return
	}
	pass.Reportf(loop.Pos(), "after close, this receive from %s returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false", v.Name())
}

func closedrecvCandidate(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) (*types.Var, ast.Node, bool) {
	if !bareLoop(loop) {
		return nil, nil, false
	}
	recv := loneRecv(pass.TypesInfo, loop.Body)
	if recv == nil {
		return nil, nil, false
	}
	v, ok := recvVar(pass, recv)
	if !ok {
		return nil, nil, false
	}
	fn := enclosingFunc(cur)
	if !isLocalVar(fn, v) || !isChan(v.Type()) {
		return nil, nil, false
	}
	return v, fn, true
}

func bareLoop(loop *ast.ForStmt) bool {
	return loop.Cond == nil && loop.Body != nil
}

func closesFor(pass *analysis.Pass, fn ast.Node, cache map[ast.Node]*fnCloses) *fnCloses {
	if fc, ok := cache[fn]; ok {
		return fc
	}
	fc := &fnCloses{reassigned: map[*types.Var]bool{}, firstClose: map[*types.Var]token.Pos{}}
	cache[fn] = fc
	if fn == nil {
		return fc
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			markReassigned(pass.TypesInfo, t, fc.reassigned)
		case *ast.RangeStmt:
			markRangeReassigned(pass.TypesInfo, t, fc.reassigned)
		}
		return true
	})
	walkCalledLit(fn, func(n ast.Node) bool {
		if t, ok := n.(*ast.GoStmt); ok {
			markGoCloses(pass, t, fc.firstClose)
		}
		return true
	})
	return fc
}

func markReassigned(info *types.Info, t *ast.AssignStmt, out map[*types.Var]bool) {
	if t.Tok != token.ASSIGN {
		return
	}
	for _, lhs := range t.Lhs {
		if id := rootIdent(lhs); id != nil {
			if v, ok := info.ObjectOf(id).(*types.Var); ok && t.Pos() > v.Pos() {
				out[v] = true
			}
		}
	}
}

func markRangeReassigned(info *types.Info, t *ast.RangeStmt, out map[*types.Var]bool) {
	if t.Tok != token.ASSIGN {
		return
	}
	for _, e := range []ast.Expr{t.Key, t.Value} {
		if id := rootIdent(e); id != nil {
			if v, ok := info.ObjectOf(id).(*types.Var); ok && t.Pos() > v.Pos() {
				out[v] = true
			}
		}
	}
}

func markGoCloses(pass *analysis.Pass, t *ast.GoStmt, out map[*types.Var]token.Pos) {
	lit, ok := spawnedBody(t)
	if !ok {
		return
	}
	for v := range closedVars(pass, lit) {
		if pos, ok := out[v]; !ok || t.Pos() < pos {
			out[v] = t.Pos()
		}
	}
}

func closedVars(pass *analysis.Pass, lit *ast.FuncLit) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	walkCalledLit(lit.Body, func(x ast.Node) bool {
		if v, ok := closedVarOf(pass, x); ok {
			out[v] = true
		}
		return true
	})
	return out
}

func closedVarOf(pass *analysis.Pass, x ast.Node) (*types.Var, bool) {
	c, ok := x.(*ast.CallExpr)
	if !ok || len(c.Args) != 1 {
		return nil, false
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok {
		return nil, false
	}
	b, ok := pass.TypesInfo.ObjectOf(id).(*types.Builtin)
	if !ok || b.Name() != "close" {
		return nil, false
	}
	arg, ok := ast.Unparen(c.Args[0]).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(arg).(*types.Var)
	return v, ok
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
