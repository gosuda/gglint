package reentrantlock

import (
	"go/ast"
	"go/constant"
	"go/token"
	"slices"
)

// A held path is retained if any continuing predecessor still holds the mutex.
// Returns are collected separately and rejoin only their own literal's caller.
// Unmodeled branch transfers cannot justify a later handoff on that path.
type pathState struct {
	held         bool
	stopped      bool
	returnedHeld bool
	opaque       bool
}

func (s pathState) join(t pathState) pathState {
	return pathState{
		held:         !s.stopped && s.held || !t.stopped && t.held,
		stopped:      s.stopped && t.stopped,
		returnedHeld: s.returnedHeld || t.returnedHeld,
		opaque:       !s.stopped && s.opaque || !t.stopped && t.opaque,
	}
}

func (f *flow) fork(walk func()) pathState {
	before := f.state
	walk()
	after := f.state
	f.state = before
	return after
}

func (f *flow) invokeLiteral(e ast.Expr) {
	lit, ok := ast.Unparen(e).(*ast.FuncLit)
	if !ok {
		return
	}
	before := f.state
	jumps := f.jumps
	f.jumps = nil
	f.state.returnedHeld = false
	f.walk(lit.Body)
	held := f.state.returnedHeld || !f.state.stopped && f.state.held
	f.state = before
	f.state.held = held
	f.jumps = jumps
}

func (f *flow) branch(s *ast.BranchStmt) {
	f.barrier()
	if s.Tok == token.FALLTHROUGH {
		return
	}
	if s.Tok != token.GOTO {
		f.state.opaque = true
		return
	}
	target := f.c.pass.TypesInfo.ObjectOf(s.Label).Pos()
	if target <= s.Pos() {
		f.state.opaque = true
		return
	}
	if f.jumps == nil {
		f.jumps = make(map[token.Pos]pathState)
	}
	if previous, ok := f.jumps[target]; ok {
		f.jumps[target] = previous.join(f.state)
	} else {
		f.jumps[target] = f.state
	}
	f.state.stopped = true
}

func (f *flow) labeled(s *ast.LabeledStmt) {
	if incoming, ok := f.jumps[s.Label.Pos()]; ok {
		f.state = f.state.join(incoming)
		delete(f.jumps, s.Label.Pos())
	}
	f.walk(s.Stmt)
}

func (f *flow) conditional(s *ast.IfStmt) {
	f.walk(s.Init)
	f.walk(s.Cond)
	if value, known := f.c.boolean(s.Cond); known {
		if value {
			f.walk(s.Body)
		} else {
			f.walk(s.Else)
		}
		return
	}
	then := f.fork(func() { f.walk(s.Body) })
	other := f.fork(func() { f.walk(s.Else) })
	f.state = then.join(other)
}

func (f *flow) shortCircuit(e *ast.BinaryExpr) {
	f.walk(e.X)
	if value, known := f.c.boolean(e.X); known {
		if value == (e.Op == token.LAND) {
			f.walk(e.Y)
		}
		return
	}
	right := f.fork(func() { f.walk(e.Y) })
	f.state = f.state.join(right)
}

func (c *checker) boolean(e ast.Expr) (truth, known bool) {
	value := c.pass.TypesInfo.Types[e].Value
	if value == nil || value.Kind() != constant.Bool {
		return false, false
	}
	return constant.BoolVal(value), true
}

func (f *flow) loop(s *ast.ForStmt) {
	f.walk(s.Init)
	f.walk(s.Cond)
	enters, known := f.c.firstIteration(s)
	if known && !enters {
		return
	}
	body := f.fork(func() {
		f.walk(s.Body)
		f.walk(s.Post)
	})
	if known && enters && !body.opaque {
		f.state = body
	} else {
		f.state = f.state.join(body)
	}
}

// This proves only entry into the first body, not a loop trip count. It covers
// modernc's constant-counter wake loop without exporting zero-iteration effects.
func (c *checker) firstIteration(s *ast.ForStmt) (enters, known bool) {
	if s.Cond == nil {
		return true, true
	}
	if value, constantCondition := c.boolean(s.Cond); constantCondition {
		return value, true
	}
	name, start := c.loopStart(s.Init)
	if start == nil {
		return false, false
	}
	cond, ok := ast.Unparen(s.Cond).(*ast.BinaryExpr)
	if !ok || !slices.Contains([]token.Token{token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ}, cond.Op) {
		return false, false
	}
	id, ok := ast.Unparen(cond.X).(*ast.Ident)
	if !ok || c.pass.TypesInfo.ObjectOf(id) != c.pass.TypesInfo.ObjectOf(name) {
		return false, false
	}
	bound := c.pass.TypesInfo.Types[cond.Y].Value
	if bound == nil {
		return false, false
	}
	return constant.Compare(start, cond.Op, bound), true
}

func (c *checker) loopStart(s ast.Stmt) (*ast.Ident, constant.Value) {
	init, ok := s.(*ast.AssignStmt)
	if !ok || (init.Tok != token.ASSIGN && init.Tok != token.DEFINE) ||
		len(init.Lhs) != 1 || len(init.Rhs) != 1 {
		return nil, nil
	}
	name, ok := ast.Unparen(init.Lhs[0]).(*ast.Ident)
	if !ok {
		return nil, nil
	}
	return name, c.pass.TypesInfo.Types[init.Rhs[0]].Value
}

func (f *flow) rangeLoop(s *ast.RangeStmt) {
	f.walk(s.X)
	body := f.fork(func() {
		f.walk(s.Key)
		f.walk(s.Value)
		f.walk(s.Body)
	})
	f.state = f.state.join(body)
}

func (f *flow) switchStmt(s *ast.SwitchStmt) {
	f.walk(s.Init)
	f.walk(s.Tag)
	tag := constant.MakeBool(true)
	if s.Tag != nil {
		tag = f.c.pass.TypesInfo.Types[s.Tag].Value
	}
	f.cases(caseClauses(s.Body), tag)
}

func fallsThrough(list []ast.Stmt) bool {
	for _, s := range slices.Backward(list) {
		for {
			label, ok := s.(*ast.LabeledStmt)
			if !ok {
				break
			}
			s = label.Stmt
		}
		if _, empty := s.(*ast.EmptyStmt); empty {
			continue
		}
		branch, ok := s.(*ast.BranchStmt)
		return ok && branch.Tok == token.FALLTHROUGH
	}
	return false
}

func caseClauses(body *ast.BlockStmt) []*ast.CaseClause {
	clauses := make([]*ast.CaseClause, 0, len(body.List))
	for _, statement := range body.List {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			panic("switch body contains a non-case statement")
		}
		clauses = append(clauses, clause)
	}
	return clauses
}

func (f *flow) cases(clauses []*ast.CaseClause, tag constant.Value) {
	entries, joined := f.caseEntries(clauses, tag)
	next := pathState{stopped: true}
	for i, clause := range clauses {
		f.state = entries[i].join(next)
		f.clauseBody(clause.Body)
		if fallsThrough(clause.Body) {
			next = f.state
		} else {
			joined = joined.join(f.state)
			next = pathState{stopped: true}
		}
	}
	f.state = joined
}

func (f *flow) caseEntries(clauses []*ast.CaseClause, tag constant.Value) ([]pathState, pathState) {
	entries := make([]pathState, len(clauses))
	defaultIndex := -1
	for i, clause := range clauses {
		if clause.List == nil {
			defaultIndex = i
		} else {
			entries[i] = f.caseExpressions(clause.List, tag)
		}
	}
	unmatched := f.state
	if defaultIndex >= 0 {
		entries[defaultIndex] = unmatched
		unmatched.stopped = true
	}
	return entries, unmatched
}

func (f *flow) caseExpressions(expressions []ast.Expr, tag constant.Value) pathState {
	matched := pathState{stopped: true, returnedHeld: f.state.returnedHeld}
	for _, expression := range expressions {
		f.walk(expression)
		matches, known := f.c.caseMatch(tag, expression)
		if !known || matches {
			matched = matched.join(f.state)
		}
		if known && matches {
			f.state.stopped = true
			break
		}
	}
	return matched
}

func (c *checker) caseMatch(tag constant.Value, expression ast.Expr) (matches, known bool) {
	value := c.pass.TypesInfo.Types[expression].Value
	if tag == nil || value == nil {
		return false, false
	}
	return constant.Compare(tag, token.EQL, value), true
}

func (f *flow) clauseBody(list []ast.Stmt) {
	if f.state.stopped {
		return
	}
	f.stmts(list)
	f.walkStmts(list)
}

func (f *flow) selectStmt(s *ast.SelectStmt) {
	clauses := commClauses(s.Body)
	for _, clause := range clauses {
		switch comm := clause.Comm.(type) {
		case *ast.SendStmt:
			f.walk(comm.Chan)
			f.walk(comm.Value)
		case *ast.ExprStmt:
			f.walk(comm.X)
		case *ast.AssignStmt:
			for _, rhs := range comm.Rhs {
				f.walk(rhs)
			}
		}
	}
	joined := pathState{stopped: true, returnedHeld: f.state.returnedHeld}
	for _, clause := range clauses {
		arm := f.fork(func() {
			if comm, ok := clause.Comm.(*ast.AssignStmt); ok {
				for _, lhs := range comm.Lhs {
					f.walk(lhs)
				}
			}
			f.clauseBody(clause.Body)
		})
		joined = joined.join(arm)
	}
	f.state = joined
}

func commClauses(body *ast.BlockStmt) []*ast.CommClause {
	clauses := make([]*ast.CommClause, 0, len(body.List))
	for _, statement := range body.List {
		clause, ok := statement.(*ast.CommClause)
		if !ok {
			panic("select body contains a non-communication statement")
		}
		clauses = append(clauses, clause)
	}
	return clauses
}
