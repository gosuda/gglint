package chanstate

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/gosuda/gglint/internal/flow"
)

type opKind int

const (
	opSend opKind = 1 << iota
	opRecv
	opClose
	opAssign
	opDefine
)

type chanOp struct {
	Kind     opKind
	Key      flow.Key
	Make     bool
	NilInit  bool
	InSelect bool
	Sel      *ast.SelectStmt
	Pos      token.Pos
	Expr     ast.Node
}

type opWalker struct {
	info  *types.Info
	key   flow.Key
	sel   *ast.SelectStmt
	inSel bool
	out   []chanOp
}

func opsOf(info *types.Info, n ast.Node, k flow.Key) []chanOp {
	w := &opWalker{info: info, key: k}
	switch n := n.(type) {
	case ast.Stmt:
		w.stmt(n)
	case *ast.ValueSpec:
		w.exprs(n.Values)
		w.valueSpec(n)
	case ast.Expr:
		w.expr(n)
	}
	return w.out
}

func (w *opWalker) stmts(l []ast.Stmt) {
	for _, s := range l {
		w.stmt(s)
	}
}

func (w *opWalker) stmt(s ast.Stmt) {
	if w.simpleStmt(s) {
		return
	}
	switch s := s.(type) {
	case *ast.SendStmt:
		w.send(s)
	case *ast.AssignStmt:
		w.assign(s)
	case *ast.DeclStmt:
		w.decl(s)
	case *ast.GoStmt:
	default:
		w.structured(s)
	}
}

func (w *opWalker) simpleStmt(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ExprStmt:
		w.expr(s.X)
	case *ast.IncDecStmt:
		w.expr(s.X)
	case *ast.ReturnStmt:
		w.exprs(s.Results)
	case *ast.DeferStmt:
		w.expr(s.Call)
	default:
		return false
	}
	return true
}

func (w *opWalker) structured(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.IfStmt:
		w.opt(s.Init)
		w.expr(s.Cond)
		w.stmt(s.Body)
		w.opt(s.Else)
	case *ast.ForStmt:
		w.opt(s.Init)
		w.expr(s.Cond)
		w.opt(s.Post)
		w.stmt(s.Body)
	case *ast.RangeStmt:
		w.rangeOver(s)
	case *ast.SelectStmt:
		w.selects(s)
	case *ast.SwitchStmt:
		w.opt(s.Init)
		w.expr(s.Tag)
		w.stmt(s.Body)
	case *ast.TypeSwitchStmt:
		w.opt(s.Init)
		w.stmt(s.Assign)
		w.stmt(s.Body)
	default:
		w.clauses(s)
	}
}

func (w *opWalker) clauses(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.CaseClause:
		w.exprs(s.List)
		w.stmts(s.Body)
	case *ast.CommClause:
		w.comm(s)
		w.stmts(s.Body)
	case *ast.BlockStmt:
		w.stmts(s.List)
	case *ast.LabeledStmt:
		w.stmt(s.Stmt)
	}
}

func (w *opWalker) comm(c *ast.CommClause) {
	if c.Comm == nil {
		return
	}
	savedIn := w.inSel
	w.inSel = true
	w.stmt(c.Comm)
	w.inSel = savedIn
}

func (w *opWalker) selects(s *ast.SelectStmt) {
	saved, savedIn := w.sel, w.inSel
	w.sel, w.inSel = s, false
	for _, c := range s.Body.List {
		cc, ok := c.(*ast.CommClause)
		if !ok {
			continue
		}
		w.comm(cc)
		w.stmts(cc.Body)
	}
	w.sel, w.inSel = saved, savedIn
}

func (w *opWalker) opt(s ast.Stmt) {
	if s != nil {
		w.stmt(s)
	}
}

func (w *opWalker) send(s *ast.SendStmt) {
	w.expr(s.Value)
	if k, ok := flow.KeyOf(w.info, s.Chan); ok && w.match(k) {
		w.out = append(w.out, chanOp{Kind: opSend, Key: k, InSelect: w.inSel, Sel: w.selField(), Pos: s.Pos(), Expr: s})
	}
}

func (w *opWalker) rangeOver(s *ast.RangeStmt) {
	k, ok := flow.KeyOf(w.info, s.X)
	switch {
	case ok && w.match(k) && isChan(w.info.TypeOf(s.X)):
		w.out = append(w.out, chanOp{Kind: opRecv, Key: k, Pos: s.X.Pos(), Expr: s.X})
	default:
		w.expr(s.X)
	}
	w.stmts(s.Body.List)
}

func (w *opWalker) assign(s *ast.AssignStmt) {
	for _, r := range s.Rhs {
		w.expr(r)
	}
	for i, l := range s.Lhs {
		k, ok := keyOfIdent(w.info, l)
		if !ok || !w.match(k) {
			continue
		}
		op := chanOp{Kind: opAssign, Key: k, InSelect: w.inSel, Sel: w.selField(), Pos: l.Pos(), Expr: l}
		if s.Tok == token.DEFINE && len(s.Rhs) == len(s.Lhs) {
			op.Kind = opDefine
			op.Make = isMake(w.info, s.Rhs[i])
		}
		w.out = append(w.out, op)
	}
}

func (w *opWalker) decl(s *ast.DeclStmt) {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		w.exprs(vs.Values)
		w.valueSpec(vs)
	}
}

func (w *opWalker) valueSpec(vs *ast.ValueSpec) {
	for _, name := range vs.Names {
		k, ok := keyOfIdent(w.info, name)
		if !ok || !w.match(k) {
			continue
		}
		w.out = append(w.out, chanOp{
			Kind:    opDefine,
			Key:     k,
			Pos:     name.Pos(),
			Expr:    name,
			Make:    len(vs.Values) == 1 && isMake(w.info, vs.Values[0]),
			NilInit: len(vs.Values) == 0,
		})
	}
}

func (w *opWalker) exprs(l []ast.Expr) {
	for _, x := range l {
		w.expr(x)
	}
}

func (w *opWalker) expr(x ast.Expr) {
	switch x := x.(type) {
	case nil:
		return
	case *ast.FuncLit:
		return
	case *ast.Ident, *ast.BasicLit, *ast.Ellipsis:
		return
	case *ast.UnaryExpr:
		w.unary(x)
	case *ast.CallExpr:
		w.call(x)
	case *ast.BinaryExpr:
		w.expr(x.X)
		w.expr(x.Y)
	default:
		w.compound(x)
	}
}

func (w *opWalker) compound(x ast.Expr) {
	if w.singleExpr(x) {
		return
	}
	switch x := x.(type) {
	case *ast.IndexExpr:
		w.indexExpr(x)
	case *ast.IndexListExpr:
		w.expr(x.X)
		w.exprs(x.Indices)
	case *ast.SliceExpr:
		w.sliceExpr(x)
	case *ast.CompositeLit:
		w.exprs(x.Elts)
	case *ast.KeyValueExpr:
		w.expr(x.Key)
		w.expr(x.Value)
	}
}

func (w *opWalker) singleExpr(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.StarExpr:
		w.expr(x.X)
	case *ast.ParenExpr:
		w.expr(x.X)
	case *ast.SelectorExpr:
		w.expr(x.X)
	case *ast.TypeAssertExpr:
		w.expr(x.X)
	default:
		return false
	}
	return true
}

func (w *opWalker) indexExpr(x *ast.IndexExpr) {
	w.expr(x.X)
	w.expr(x.Index)
}

func (w *opWalker) sliceExpr(x *ast.SliceExpr) {
	w.expr(x.X)
	w.optExpr(x.Low)
	w.optExpr(x.High)
	w.optExpr(x.Max)
}

func (w *opWalker) unary(x *ast.UnaryExpr) {
	if x.Op == token.ARROW {
		if k, ok := flow.KeyOf(w.info, x.X); ok && w.match(k) {
			w.out = append(w.out, chanOp{Kind: opRecv, Key: k, InSelect: w.inSel, Sel: w.selField(), Pos: x.Pos(), Expr: x})
			return
		}
	}
	w.expr(x.X)
}

func (w *opWalker) call(c *ast.CallExpr) {
	if k, ok := closeKey(w.info, c); ok && w.match(k) {
		w.out = append(w.out, chanOp{Kind: opClose, Key: k, InSelect: w.inSel, Sel: w.selField(), Pos: c.Pos(), Expr: c})
		return
	}
	w.expr(c.Fun)
	w.exprs(c.Args)
}

func (w *opWalker) optExpr(x ast.Expr) {
	if x != nil {
		w.expr(x)
	}
}

func (w *opWalker) match(k flow.Key) bool {
	return w.key.Root == nil || k == w.key
}

func (w *opWalker) selField() *ast.SelectStmt {
	if w.inSel {
		return w.sel
	}
	return nil
}

func closeKey(info *types.Info, c *ast.CallExpr) (flow.Key, bool) {
	if c == nil || len(c.Args) != 1 {
		return flow.Key{}, false
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok {
		return flow.Key{}, false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	if !ok || b.Name() != "close" {
		return flow.Key{}, false
	}
	return flow.KeyOf(info, c.Args[0])
}

func isMake(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "make"
}

func constInt(info *types.Info, e ast.Expr) (int64, bool) {
	e = ast.Unparen(e)
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(tv.Value)
}

func constCap(info *types.Info, call *ast.CallExpr) (int64, bool) {
	if len(call.Args) < 2 {
		return 0, true
	}
	return constInt(info, call.Args[1])
}

func isChan(t types.Type) bool {
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func chanName(k flow.Key) string {
	return k.Root.Name()
}

func keyOfIdent(info *types.Info, e ast.Expr) (flow.Key, bool) {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		if v, ok := info.Defs[id].(*types.Var); ok {
			return flow.Key{Root: v}, true
		}
	}
	return flow.KeyOf(info, e)
}
