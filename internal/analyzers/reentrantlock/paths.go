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
	f.state.returnedHeld = false
	f.walk(lit.Body)
	held := f.state.returnedHeld || !f.state.stopped && f.state.held
	f.state = before
	f.state.held = held
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

func (c *checker) boolean(e ast.Expr) (bool, bool) {
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
func (c *checker) firstIteration(s *ast.ForStmt) (bool, bool) {
	if s.Cond == nil {
		return true, true
	}
	if value, known := c.boolean(s.Cond); known {
		return value, true
	}
	init, ok := s.Init.(*ast.AssignStmt)
	if !ok || (init.Tok != token.ASSIGN && init.Tok != token.DEFINE) ||
		len(init.Lhs) != 1 || len(init.Rhs) != 1 {
		return false, false
	}
	name, ok := ast.Unparen(init.Lhs[0]).(*ast.Ident)
	if !ok {
		return false, false
	}
	cond, ok := ast.Unparen(s.Cond).(*ast.BinaryExpr)
	if !ok {
		return false, false
	}
	switch cond.Op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
	default:
		return false, false
	}
	id, ok := ast.Unparen(cond.X).(*ast.Ident)
	if !ok || c.pass.TypesInfo.ObjectOf(id) != c.pass.TypesInfo.ObjectOf(name) {
		return false, false
	}
	start := c.pass.TypesInfo.Types[init.Rhs[0]].Value
	bound := c.pass.TypesInfo.Types[cond.Y].Value
	if start == nil || bound == nil {
		return false, false
	}
	return constant.Compare(start, cond.Op, bound), true
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
	if choice, known := f.c.constantCase(s); known {
		for choice >= 0 && choice < len(s.Body.List) && !f.state.stopped {
			clause := s.Body.List[choice].(*ast.CaseClause)
			f.clauseBody(clause.Body)
			if !fallsThrough(clause.Body) {
				break
			}
			choice++
		}
		return
	}
	f.cases(s.Body.List)
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

func (c *checker) constantCase(s *ast.SwitchStmt) (int, bool) {
	tag := constant.MakeBool(true)
	if s.Tag != nil {
		tag = c.pass.TypesInfo.Types[s.Tag].Value
		if tag == nil {
			return 0, false
		}
	}
	choice := -1
	for i, node := range s.Body.List {
		clause := node.(*ast.CaseClause)
		if clause.List == nil {
			choice = i
		}
		for _, expr := range clause.List {
			value := c.pass.TypesInfo.Types[expr].Value
			if value == nil {
				return 0, false
			}
			if constant.Compare(tag, token.EQL, value) {
				return i, true
			}
		}
	}
	return choice, true
}

func (f *flow) cases(list []ast.Stmt) {
	joined := pathState{stopped: true, returnedHeld: f.state.returnedHeld}
	defaultCase := false
	for _, node := range list {
		clause := node.(*ast.CaseClause)
		defaultCase = defaultCase || clause.List == nil
		for _, expr := range clause.List {
			f.fork(func() { f.walk(expr) })
		}
		arm := f.fork(func() { f.clauseBody(clause.Body) })
		joined = joined.join(arm)
	}
	if !defaultCase {
		joined = joined.join(f.state)
	}
	f.state = joined
}

func (f *flow) clauseBody(list []ast.Stmt) {
	if f.state.stopped {
		return
	}
	f.stmts(list)
	f.walkStmts(list)
}

func (f *flow) selectStmt(s *ast.SelectStmt) {
	for _, node := range s.Body.List {
		clause := node.(*ast.CommClause)
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
	for _, node := range s.Body.List {
		clause := node.(*ast.CommClause)
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
