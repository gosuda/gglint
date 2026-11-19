package chanblock

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// timerUse tracks one local variable initialized with time.NewTimer or
// time.NewTicker. The constructor is known before the walk; the rest is
// collected in source order while walking: defAt is the function depth,
// defIn and reset the innermost for statement, deferred holds the depths of
// the defer statements that exempt its Stop calls, src is the defining
// variable, and taint marks a mention outside the four allowed forms. Port
// of the timer side of loGos correctness::unbounded_channel_op.
type timerUse struct {
	ctor     string
	def      bool
	defAt    int
	defIn    ast.Node
	recv     bool
	reset    ast.Node
	resetAt  token.Pos
	deferred []int
	src      *types.Var
	taint    bool
}

func (u *unit) timersInit(fn ast.Node) {
	u.timers = make(map[*types.Var]*timerUse)
	u.tdefs = make(map[*types.Var]int)
	u.collectTimers(fn)
	u.taintTimers(fn)
	u.reruns = backJumps(fn)
}

// collectTimers records definition counts and the constructor of every
// tracked timer, before any select is judged.
func (u *unit) collectTimers(fn ast.Node) {
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.ASSIGN || n.Tok == token.DEFINE {
				u.noteTimers(n.Lhs, n.Rhs)
			}
		case *ast.ValueSpec:
			if len(n.Values) == 0 {
				return true
			}
			lhs := make([]ast.Expr, len(n.Names))
			for i, name := range n.Names {
				lhs[i] = name
			}
			u.noteTimers(lhs, n.Values)
		}
		return true
	})
}

func (u *unit) noteTimers(lhs, rhs []ast.Expr) {
	for i, l := range lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		v := localVar(u.info, id)
		if v == nil {
			continue
		}
		u.tdefs[v]++
		if len(rhs) != len(lhs) {
			continue
		}
		u.trackTimer(v, rhs[i])
	}
}

func (u *unit) trackTimer(v *types.Var, x ast.Expr) {
	if ctor := timerCtor(u.info, x); ctor != "" {
		u.timers[v] = &timerUse{ctor: ctor, src: v}
		return
	}
	aid, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return
	}
	w := localVar(u.info, aid)
	if w == nil {
		return
	}
	if tw := u.timers[w]; tw != nil {
		u.timers[v] = tw
	}
}

// timerCtor returns NewTimer or NewTicker when x calls that function of
// package time, or "".
func timerCtor(info *types.Info, x ast.Expr) string {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok {
		return ""
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || (fn.Name() != "NewTimer" && fn.Name() != "NewTicker") {
		return ""
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || fn.Pkg() == nil || fn.Pkg().Path() != "time" {
		return ""
	}
	return fn.Name()
}

// taintTimers marks the tracked timers mentioned outside the four allowed
// forms, before any select is judged. Allowed are the declaring statement,
// defer t.Stop(), a t.Reset standing alone as a statement at the top of a
// loop body, and the operand t.C of a receive or a range; every other
// mention, including a read or assignment of t.C, taints.
func (u *unit) taintTimers(fn ast.Node) {
	ast.Inspect(fn, u.taintVisit)
}

func (u *unit) taintWalk(n ast.Node) {
	if n != nil {
		ast.Inspect(n, u.taintVisit)
	}
}

func (u *unit) taintVisit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.FuncLit:
		u.taintFuncLit(n)
		return false
	case *ast.Ident:
		u.taintTimer(n)
	case *ast.SelectorExpr, *ast.UnaryExpr:
		return u.taintOperand(n)
	case *ast.RangeStmt:
		u.taintRange(n)
		return false
	case *ast.DeferStmt:
		u.deferMention(n.Call)
		return false
	case *ast.ForStmt:
		u.taintForBody(n)
		return false
	}
	return true
}

// taintOperand handles mentions inside expressions: a selector taints its
// tracked receiver, and a receive operand is spared when it is a timer C.
func (u *unit) taintOperand(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		if t := u.timerVar(n.X); t != nil {
			t.taint = true
		}
	case *ast.UnaryExpr:
		if n.Op == token.ARROW {
			u.allowRecvOperand(n.X)
			return false
		}
	}
	return true
}

func (u *unit) taintFuncLit(n *ast.FuncLit) {
	u.depth++
	u.taintWalk(n.Body)
	u.depth--
}

func (u *unit) taintRange(n *ast.RangeStmt) {
	u.allowRecvOperand(n.X)
	u.taintWalk(n.Key)
	u.taintWalk(n.Value)
	u.taintBodyList(bodyList(n.Body))
}

// taintTimer taints the tracked timer the name uses, if any. A defining
// occurrence is not a use, so the declaring statement does not taint.
func (u *unit) taintTimer(id *ast.Ident) {
	v, ok := u.info.Uses[id].(*types.Var)
	if !ok {
		return
	}
	if t := u.timers[v]; t != nil {
		t.taint = true
	}
}

// allowRecvOperand spares the C field of a tracked timer when x is the
// operand of a receive, and walks x as ordinary code otherwise.
func (u *unit) allowRecvOperand(x ast.Expr) {
	if sel, ok := ast.Unparen(x).(*ast.SelectorExpr); ok && sel.Sel.Name == "C" && u.timerVar(sel.X) != nil {
		return
	}
	u.taintWalk(x)
}

// deferMention spares the plain Stop call of a defer statement, records the
// depth of its function, and walks every other mention under the statement
// as ordinary code.
func (u *unit) deferMention(call *ast.CallExpr) {
	if t, plain := u.deferStop(call); plain {
		t.deferred = append(t.deferred, u.depth)
	} else {
		u.taintWalk(call)
	}
}

// deferStop reports the tracked timer of a plain t.Stop() call that names
// its defining variable, for the top call of a defer statement.
func (u *unit) deferStop(call *ast.CallExpr) (*timerUse, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Stop" {
		return nil, false
	}
	t := u.timerVar(sel.X)
	if t == nil {
		return nil, false
	}
	id, plain := ast.Unparen(sel.X).(*ast.Ident)
	return t, plain && timerName(u.info, id) == t.src
}

// taintForBody walks a for statement: the header as ordinary code, a
// qualifying top-of-body Reset through its arguments only, and every other
// direct statement as ordinary code.
func (u *unit) taintForBody(f *ast.ForStmt) {
	u.taintWalk(f.Init)
	u.taintWalk(f.Cond)
	u.taintWalk(f.Post)
	u.taintBodyList(bodyList(f.Body))
}

func (u *unit) taintBodyList(list []ast.Stmt) {
	for _, st := range list {
		if u.taintTopReset(st) {
			continue
		}
		u.taintWalk(st)
	}
}

// taintTopReset walks the arguments of a top-of-body Reset without tainting
// the call itself, and reports whether st was one.
func (u *unit) taintTopReset(st ast.Stmt) bool {
	v := topReset(u.info, st)
	if v == nil || u.timers[v] == nil {
		return false
	}
	u.taintResetArgs(st)
	return true
}

func (u *unit) taintResetArgs(st ast.Stmt) {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return
	}
	for _, a := range call.Args {
		u.taintWalk(a)
	}
}

func bodyList(b *ast.BlockStmt) []ast.Stmt {
	if b == nil {
		return nil
	}
	return b.List
}

// topReset returns the variable a loop body statement rearms: a bare t.Reset
// call standing alone as the statement, or nil.
func topReset(info *types.Info, st ast.Stmt) *types.Var {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Reset" {
		return nil
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return nil
	}
	return localVar(info, id)
}

// timerName returns the variable that x names, through parentheses and
// pointer indirection, or nil.
func timerName(info *types.Info, x ast.Expr) *types.Var {
	for {
		x = ast.Unparen(x)
		switch op := x.(type) {
		case *ast.StarExpr:
			x = op.X
		case *ast.UnaryExpr:
			if op.Op != token.AND {
				return nil
			}
			x = op.X
		default:
			id, ok := x.(*ast.Ident)
			if !ok {
				return nil
			}
			return localVar(info, id)
		}
	}
}

// timerVar returns the tracked timer whose receiver x names, or nil.
func (u *unit) timerVar(x ast.Expr) *timerUse {
	return u.timers[timerName(u.info, x)]
}

type span struct{ from, to token.Pos }

func (s span) holds(pos token.Pos) bool {
	return s.from <= pos && pos <= s.to
}

// backJumps returns the spans of backward gotos: a goto whose label sits
// before it reruns the statements between the label and the jump. Labels
// are scoped to their function literal, as goto cannot cross one.
func backJumps(fn ast.Node) []span {
	b := &jumps{labels: []map[string]token.Pos{{}}}
	ast.Inspect(fn, b.visit)
	return b.out
}

type jumps struct {
	out    []span
	labels []map[string]token.Pos
}

func (b *jumps) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.FuncLit:
		b.labels = append(b.labels, map[string]token.Pos{})
		if n.Body != nil {
			ast.Inspect(n.Body, b.visit)
		}
		b.labels = b.labels[:len(b.labels)-1]
		return false
	case *ast.LabeledStmt:
		b.labels[len(b.labels)-1][n.Label.Name] = n.Pos()
	case *ast.BranchStmt:
		if n.Tok != token.GOTO {
			return true
		}
		if at, ok := b.labels[len(b.labels)-1][n.Label.Name]; ok && at < n.Pos() {
			b.out = append(b.out, span{from: at, to: n.Pos()})
		}
	}
	return true
}

func (u *unit) rerunsWithout(stmt, armed token.Pos) bool {
	for _, s := range u.reruns {
		if s.holds(stmt) && !s.holds(armed) {
			return true
		}
	}
	return false
}

// timerDefs records the declaration of a tracked timer in lhs. Defs holds a
// name only when the statement declares it, so a plain assignment, a
// parameter, or a variable of an enclosing function declares nothing.
func (u *unit) timerDefs(lhs []ast.Expr) {
	for _, l := range lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := u.info.Defs[id].(*types.Var)
		if !ok || localVar(u.info, id) == nil {
			continue
		}
		if t := u.timers[v]; t != nil && !t.def {
			t.def = true
			t.defAt = u.depth
			t.defIn = u.loopAt()
		}
	}
}

// loopAt returns the innermost for or range statement open at the current
// node, or nil.
func (u *unit) loopAt() ast.Node {
	if len(u.forStack) == 0 {
		return nil
	}
	return u.forStack[len(u.forStack)-1]
}

// timerDrain marks the tracked timer a receive from x, a C field, drains.
func (u *unit) timerDrain(x ast.Expr) {
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "C" {
		return
	}
	if t := u.timerVar(sel.X); t != nil {
		t.recv = true
	}
}

func (u *unit) selectCovered(s *ast.SelectStmt) bool {
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok || cc.Comm == nil {
			continue
		}
		if r := commReceive(cc.Comm); r != nil && u.isTimeoutRecv(s.Pos(), r.X) {
			return true
		}
	}
	return false
}

// isTimeoutRecv reports whether x is a receive that bounds a select: a call
// of time.After, or the C field of a timer or ticker that the function
// creates with time.NewTimer or time.NewTicker and has not stopped.
func (u *unit) isTimeoutRecv(at token.Pos, x ast.Expr) bool {
	if call, ok := ast.Unparen(x).(*ast.CallExpr); ok {
		return isTimeAfter(u.info, call)
	}
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "C" {
		return false
	}
	return u.localTimerBound(at, sel)
}

// localTimerBound reports whether sel ends the wait of the select at
// position at: the timer is a local of the function the walk is under,
// declared once and never assigned again, not stopped, and for a timer, not
// received from and not waiting in a for statement that neither declares it
// nor resets it. A deferred Stop exempts only its own function.
func (u *unit) localTimerBound(at token.Pos, sel *ast.SelectorExpr) bool {
	v := timerName(u.info, sel.X)
	if v == nil {
		return false
	}
	t := u.timers[v]
	if !u.timerDeclared(t, v) {
		return false
	}
	if u.timerStoppedElsewhere(t) {
		return false
	}
	if t.ctor == "NewTicker" {
		return true
	}
	armed, ok := t.armedFor(u.loopAt())
	return ok && !t.recv && !u.rerunsWithout(at, armed)
}

// timerDeclared reports whether t is a single-assignment timer local to the
// function depth the walk is under.
func (u *unit) timerDeclared(t *timerUse, v *types.Var) bool {
	return t != nil && t.def && t.defAt == u.depth &&
		u.tdefs[v] == 1 && u.tdefs[t.src] == 1 && !t.taint
}

// timerStoppedElsewhere reports whether a Stop of t is deferred at a
// function depth other than the one the walk is under.
func (u *unit) timerStoppedElsewhere(t *timerUse) bool {
	for _, d := range t.deferred {
		if d != u.depth {
			return true
		}
	}
	return false
}

func (t *timerUse) armedFor(loop ast.Node) (token.Pos, bool) {
	if loop != nil && t.reset == loop {
		return t.resetAt, true
	}
	return t.src.Pos(), loop == nil || t.defIn == loop
}

// isTimeAfter reports whether call invokes the function After of package
// time, not a method or a function of another package named After.
func isTimeAfter(info *types.Info, call *ast.CallExpr) bool {
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || fn.Name() != "After" || fn.Pkg() == nil || fn.Pkg().Path() != "time" {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() == nil
}

func hasDefaultCase(s *ast.SelectStmt) bool {
	for _, stmt := range s.Body.List {
		if cc, ok := stmt.(*ast.CommClause); ok && cc.Comm == nil {
			return true
		}
	}
	return false
}
