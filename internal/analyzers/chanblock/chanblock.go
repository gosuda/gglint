package chanblock

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/inspection"
)

const (
	sendMessage    = "channel send can block forever: select on ctx.Done() or use a buffered channel"
	receiveMessage = "channel receive can block forever: select on ctx.Done()"
)

var Analyzer = &analysis.Analyzer{
	Name: "chanblock",
	Doc: `report channel sends and receives that can block forever

A channel operation is bounded only when it is a case of a select that also
receives from x.Done(), or when its channel is a local variable made once in
the function by make(chan T, k) with a constant k and the function, nested
function literals included, sends on it at most k times and never inside a
loop. When a send on such a channel sits in a loop or the sends exceed k,
every send and receive on the channel is reported. A receive of x.Done() on
its own, range over a channel, close, len, and cap are not reported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	ins := inspection.Of(pass)
	roots := []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}
	ins.Nodes(roots, func(n ast.Node, push bool) bool {
		if push {
			check(pass, funcBody(n))
		}
		return false
	})
	return nil, nil
}

func funcBody(n ast.Node) *ast.BlockStmt {
	switch n := n.(type) {
	case *ast.FuncDecl:
		return n.Body
	case *ast.FuncLit:
		return n.Body
	}
	return nil
}

func check(pass *analysis.Pass, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	u := &unit{
		info:   pass.TypesInfo,
		sent:   make(map[*types.Var]int),
		looped: make(map[*types.Var]bool),
	}
	u.walk(body)
	if len(u.ops) == 0 {
		return
	}
	b := collectBounds(pass.TypesInfo, body)
	for _, op := range u.ops {
		if op.ch != nil && u.bounded(b, op.ch) {
			continue
		}
		pass.Report(analysis.Diagnostic{Pos: op.node.Pos(), End: op.node.End(), Message: op.message})
	}
}

type operation struct {
	node    ast.Node
	ch      *types.Var
	message string
}

type unit struct {
	info    *types.Info
	loops   int
	covered bool
	sent    map[*types.Var]int
	looped  map[*types.Var]bool
	ops     []operation
}

func (u *unit) bounded(b bounds, ch *types.Var) bool {
	k, ok := b.caps[ch]
	return ok && b.defs[ch] == 1 && !u.looped[ch] && int64(u.sent[ch]) <= k
}

func (u *unit) walk(n ast.Node) {
	if n != nil {
		ast.Inspect(n, u.visit)
	}
}

func (u *unit) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.ForStmt:
		u.forStmt(n)
		return false
	case *ast.RangeStmt:
		u.rangeStmt(n)
		return false
	case *ast.SelectStmt:
		u.selectStmt(n)
		return false
	case *ast.SendStmt:
		u.send(n)
	case *ast.UnaryExpr:
		u.unary(n)
	}
	return true
}

func (u *unit) forStmt(f *ast.ForStmt) {
	u.walk(f.Init)
	u.loops++
	u.walk(f.Cond)
	u.walk(f.Post)
	u.walk(f.Body)
	u.loops--
}

func (u *unit) rangeStmt(r *ast.RangeStmt) {
	u.walk(r.X)
	u.loops++
	u.walk(r.Key)
	u.walk(r.Value)
	u.walk(r.Body)
	u.loops--
}

func (u *unit) selectStmt(s *ast.SelectStmt) {
	done := u.hasDoneCase(s)
	for _, stmt := range s.Body.List {
		cc := inspection.As[*ast.CommClause](stmt)
		u.covered = done
		operands := u.comm(cc.Comm)
		u.covered = false
		for _, x := range operands {
			u.walk(x)
		}
		for _, st := range cc.Body {
			u.walk(st)
		}
	}
}

func (u *unit) hasDoneCase(s *ast.SelectStmt) bool {
	for _, stmt := range s.Body.List {
		r := commReceive(inspection.As[*ast.CommClause](stmt).Comm)
		if r != nil && u.isDone(r.X) {
			return true
		}
	}
	return false
}

func (u *unit) comm(st ast.Stmt) []ast.Node {
	switch st := st.(type) {
	case *ast.SendStmt:
		u.send(st)
		return []ast.Node{st.Chan, st.Value}
	case *ast.ExprStmt:
		return u.commOperands(st.X)
	case *ast.AssignStmt:
		return append(u.commOperands(st.Rhs[0]), nodes(st.Lhs)...)
	}
	return nil
}

func (u *unit) commOperands(x ast.Expr) []ast.Node {
	r := receiveOf(x)
	if r == nil {
		return []ast.Node{x}
	}
	u.receive(r)
	return []ast.Node{r.X}
}

func (u *unit) send(s *ast.SendStmt) {
	ch := u.local(s.Chan)
	if ch != nil {
		u.countSend(ch)
	}
	u.record(s, ch, sendMessage)
}

func (u *unit) countSend(ch *types.Var) {
	if u.loops > 0 {
		u.looped[ch] = true
		return
	}
	u.sent[ch]++
}

func (u *unit) unary(x *ast.UnaryExpr) {
	if x.Op == token.ARROW {
		u.receive(x)
	}
}

func (u *unit) receive(r *ast.UnaryExpr) {
	if u.isDone(r.X) {
		return
	}
	u.record(r, u.local(r.X), receiveMessage)
}

func (u *unit) record(n ast.Node, ch *types.Var, message string) {
	if !u.covered {
		u.ops = append(u.ops, operation{node: n, ch: ch, message: message})
	}
}

func (u *unit) local(x ast.Expr) *types.Var {
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return nil
	}
	return localVar(u.info, id)
}

func (u *unit) isDone(x ast.Expr) bool {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Done" {
		return false
	}
	selection, ok := u.info.Selections[sel]
	return ok && selection.Kind() == types.MethodVal && isDoneChan(u.info.TypeOf(call))
}

func isDoneChan(t types.Type) bool {
	if t == nil {
		return false
	}
	ch, ok := t.Underlying().(*types.Chan)
	if !ok || ch.Dir() != types.RecvOnly {
		return false
	}
	elem, ok := ch.Elem().Underlying().(*types.Struct)
	return ok && elem.NumFields() == 0
}

func commReceive(st ast.Stmt) *ast.UnaryExpr {
	switch st := st.(type) {
	case *ast.ExprStmt:
		return receiveOf(st.X)
	case *ast.AssignStmt:
		return receiveOf(st.Rhs[0])
	}
	return nil
}

func receiveOf(x ast.Expr) *ast.UnaryExpr {
	r, ok := ast.Unparen(x).(*ast.UnaryExpr)
	if !ok || r.Op != token.ARROW {
		return nil
	}
	return r
}

func nodes(xs []ast.Expr) []ast.Node {
	ns := make([]ast.Node, len(xs))
	for i, x := range xs {
		ns[i] = x
	}
	return ns
}

func localVar(info *types.Info, id *ast.Ident) *types.Var {
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok || v.IsField() || v.Pkg() == nil || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
		return nil
	}
	return v
}

type bounds struct {
	info *types.Info
	defs map[*types.Var]int
	caps map[*types.Var]int64
}

func collectBounds(info *types.Info, body *ast.BlockStmt) bounds {
	b := bounds{
		info: info,
		defs: make(map[*types.Var]int),
		caps: make(map[*types.Var]int64),
	}
	ast.Inspect(body, b.visit)
	return b
}

func (b bounds) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		b.note(n.Lhs, n.Rhs)
	case *ast.ValueSpec:
		b.noteSpec(n)
	case *ast.RangeStmt:
		b.note([]ast.Expr{n.Key, n.Value}, nil)
	}
	return true
}

func (b bounds) noteSpec(spec *ast.ValueSpec) {
	if len(spec.Values) == 0 {
		return
	}
	lhs := make([]ast.Expr, len(spec.Names))
	for i, name := range spec.Names {
		lhs[i] = name
	}
	b.note(lhs, spec.Values)
}

func (b bounds) note(lhs, rhs []ast.Expr) {
	for i, l := range lhs {
		id, ok := ast.Unparen(l).(*ast.Ident)
		if !ok {
			continue
		}
		v := localVar(b.info, id)
		if v == nil {
			continue
		}
		b.defs[v]++
		if len(rhs) == len(lhs) {
			b.noteCapacity(v, rhs[i])
		}
	}
}

func (b bounds) noteCapacity(v *types.Var, x ast.Expr) {
	if k, ok := capacity(b.info, x); ok {
		b.caps[v] = k
	}
}

func capacity(info *types.Info, x ast.Expr) (int64, bool) {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok || !isMakeChan(info, call) {
		return 0, false
	}
	if len(call.Args) < 2 {
		return 0, true
	}
	value := info.Types[call.Args[1]].Value
	if value == nil {
		return -1, true
	}
	k, exact := constant.Int64Val(constant.ToInt(value))
	if !exact {
		return -1, true
	}
	return k, true
}

func isMakeChan(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	builtin, ok := info.Uses[id].(*types.Builtin)
	if !ok || builtin.Name() != "make" {
		return false
	}
	t := info.TypeOf(call)
	if t == nil {
		return false
	}
	_, ok = t.Underlying().(*types.Chan)
	return ok
}
