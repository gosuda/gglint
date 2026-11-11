package chanstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/gosuda/gglint/internal/flow"
)

func handsOnWith(info *types.Info, fn ast.Node, k flow.Key, spawned map[*ast.FuncLit]bool) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	w := &handWalker{info: info, key: k, fn: fn, spawned: spawned}
	return w.list(body.List, false)
}

type handWalker struct {
	info    *types.Info
	key     flow.Key
	fn      ast.Node
	spawned map[*ast.FuncLit]bool
}

func (w *handWalker) list(l []ast.Stmt, esc bool) bool {
	for _, s := range l {
		if w.stmt(s, esc) {
			return true
		}
	}
	return false
}

func (w *handWalker) opt(s ast.Stmt, esc bool) bool {
	return s != nil && w.stmt(s, esc)
}

func (w *handWalker) stmt(s ast.Stmt, esc bool) bool {
	if res, ok := w.spawnStmt(s); ok {
		return res
	}
	switch s := s.(type) {
	case *ast.ExprStmt:
		return w.expr(s.X, esc)
	case *ast.IncDecStmt:
		return w.expr(s.X, esc)
	case *ast.AssignStmt:
		return w.assign(s, esc)
	case *ast.DeclStmt:
		return w.decl(s, esc)
	default:
		return w.structured(s, esc)
	}
}

func (w *handWalker) spawnStmt(s ast.Stmt) (res, handled bool) {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return w.exprs(s.Results, true), true
	case *ast.GoStmt:
		return w.expr(s.Call, true), true
	case *ast.DeferStmt:
		return w.expr(s.Call, true), true
	case *ast.SendStmt:
		return w.expr(s.Value, true), true
	}
	return false, false
}

func (w *handWalker) structured(s ast.Stmt, esc bool) bool {
	if res, ok := w.plainStmt(s, esc); ok {
		return res
	}
	switch s := s.(type) {
	case *ast.IfStmt:
		return w.condIf(s, esc)
	case *ast.ForStmt:
		return w.condFor(s, esc)
	case *ast.RangeStmt:
		return w.condRange(s, esc)
	case *ast.SwitchStmt:
		return w.condSwitch(s, esc)
	case *ast.TypeSwitchStmt:
		return w.condTypeSwitch(s, esc)
	default:
		return w.clause(s, esc)
	}
}

func (w *handWalker) plainStmt(s ast.Stmt, esc bool) (res, handled bool) {
	switch s := s.(type) {
	case *ast.BlockStmt:
		return w.list(s.List, esc), true
	case *ast.LabeledStmt:
		return w.stmt(s.Stmt, esc), true
	case *ast.SelectStmt:
		return w.clauses(s.Body.List, esc), true
	}
	return false, false
}

func (w *handWalker) condIf(s *ast.IfStmt, esc bool) bool {
	if w.opt(s.Init, esc) {
		return true
	}
	if w.optExpr(s.Cond, esc) {
		return true
	}
	if w.list(s.Body.List, esc) {
		return true
	}
	return w.opt(s.Else, esc)
}

func (w *handWalker) condFor(s *ast.ForStmt, esc bool) bool {
	if w.opt(s.Init, esc) {
		return true
	}
	if w.optExpr(s.Cond, esc) {
		return true
	}
	if w.opt(s.Post, esc) {
		return true
	}
	return w.list(s.Body.List, esc)
}

func (w *handWalker) condRange(s *ast.RangeStmt, esc bool) bool {
	if w.optExpr(s.X, esc) {
		return true
	}
	return w.list(s.Body.List, esc)
}

func (w *handWalker) condSwitch(s *ast.SwitchStmt, esc bool) bool {
	if w.opt(s.Init, esc) {
		return true
	}
	if w.optExpr(s.Tag, esc) {
		return true
	}
	return w.clauses(s.Body.List, esc)
}

func (w *handWalker) condTypeSwitch(s *ast.TypeSwitchStmt, esc bool) bool {
	if w.opt(s.Init, esc) {
		return true
	}
	if w.stmt(s.Assign, esc) {
		return true
	}
	return w.clauses(s.Body.List, esc)
}

func (w *handWalker) clauses(l []ast.Stmt, esc bool) bool {
	for _, s := range l {
		if w.clause(s, esc) {
			return true
		}
	}
	return false
}

func (w *handWalker) clause(s ast.Stmt, esc bool) bool {
	switch s := s.(type) {
	case *ast.CaseClause:
		return w.exprs(s.List, esc) || w.list(s.Body, esc)
	case *ast.CommClause:
		return w.opt(s.Comm, esc) || w.list(s.Body, esc)
	}
	return false
}

func (w *handWalker) assign(s *ast.AssignStmt, esc bool) bool {
	rhs := esc || w.stores(s.Lhs)
	if w.exprs(s.Rhs, rhs) {
		return true
	}
	if w.exprs(s.Lhs, false) {
		return true
	}
	return rhs && w.storedKey(s.Lhs)
}

func (w *handWalker) storedKey(lhs []ast.Expr) bool {
	for _, l := range lhs {
		idx, ok := ast.Unparen(l).(*ast.IndexExpr)
		if !ok {
			continue
		}
		if w.expr(idx.Index, true) {
			return true
		}
	}
	return false
}

func (w *handWalker) decl(s *ast.DeclStmt, esc bool) bool {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok && w.exprs(vs.Values, esc) {
			return true
		}
	}
	return false
}

func (w *handWalker) exprs(l []ast.Expr, esc bool) bool {
	for _, x := range l {
		if w.expr(x, esc) {
			return true
		}
	}
	return false
}

func (w *handWalker) optExpr(x ast.Expr, esc bool) bool {
	return x != nil && w.expr(x, esc)
}

func (w *handWalker) expr(x ast.Expr, esc bool) bool {
	switch x := x.(type) {
	case nil:
		return false
	case *ast.Ident:
		if !esc {
			return false
		}
		return w.match(x)
	case *ast.FuncLit:
		return w.literal(x, esc)
	case *ast.UnaryExpr:
		return w.unary(x, esc)
	case *ast.CallExpr:
		return w.call(x, esc)
	case *ast.BinaryExpr:
		return w.binaryExpr(x, esc)
	default:
		return w.compound(x, esc)
	}
}

func (w *handWalker) binaryExpr(x *ast.BinaryExpr, esc bool) bool {
	if w.expr(x.X, esc) {
		return true
	}
	return w.expr(x.Y, esc)
}

func (w *handWalker) literal(lit *ast.FuncLit, esc bool) bool {
	if w.spawned[lit] {
		return w.list(lit.Body.List, false)
	}
	if mentionsKey(w.info, lit, w.key) {
		return true
	}
	return w.list(lit.Body.List, esc)
}

func (w *handWalker) match(id *ast.Ident) bool {
	k, ok := keyOfIdent(w.info, id)
	return ok && k == w.key
}

func (w *handWalker) unary(x *ast.UnaryExpr, esc bool) bool {
	switch x.Op {
	case token.ARROW:
		return w.expr(x.X, false)
	case token.AND:
		return w.expr(x.X, true)
	case token.ILLEGAL, token.EOF, token.COMMENT, token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING, token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.OR, token.XOR, token.SHL, token.SHR, token.AND_NOT, token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN, token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN, token.AND_NOT_ASSIGN, token.LAND, token.LOR, token.INC, token.DEC, token.EQL, token.LSS, token.GTR, token.ASSIGN, token.NOT, token.NEQ, token.LEQ, token.GEQ, token.DEFINE, token.ELLIPSIS, token.LPAREN, token.LBRACK, token.LBRACE, token.COMMA, token.PERIOD, token.RPAREN, token.RBRACK, token.RBRACE, token.SEMICOLON, token.COLON, token.BREAK, token.CASE, token.CHAN, token.CONST, token.CONTINUE, token.DEFAULT, token.DEFER, token.ELSE, token.FALLTHROUGH, token.FOR, token.FUNC, token.GO, token.GOTO, token.IF, token.IMPORT, token.INTERFACE, token.MAP, token.PACKAGE, token.RANGE, token.RETURN, token.SELECT, token.STRUCT, token.SWITCH, token.TYPE, token.VAR, token.TILDE:
		return w.expr(x.X, esc)
	}
	return w.expr(x.X, esc)
}

func (w *handWalker) call(c *ast.CallExpr, esc bool) bool {
	if _, ok := closeKey(w.info, c); ok {
		return w.expr(c.Args[0], false)
	}
	if w.plain(c) {
		return w.expr(c.Fun, esc) || w.exprs(c.Args, esc)
	}
	return w.expr(c.Fun, esc) || w.exprs(c.Args, true)
}

func (w *handWalker) plain(c *ast.CallExpr) bool {
	if w.info.Types[c.Fun].IsType() {
		return true
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok || (id.Name != "len" && id.Name != "cap") {
		return false
	}
	b, ok := w.info.Uses[id].(*types.Builtin)
	return ok && (b.Name() == "len" || b.Name() == "cap")
}

func (w *handWalker) compound(x ast.Expr, esc bool) bool {
	if res, ok := w.singleCompound(x, esc); ok {
		return res
	}
	return w.indexCompound(x, esc)
}

func (w *handWalker) singleCompound(x ast.Expr, esc bool) (res, handled bool) {
	switch x := x.(type) {
	case *ast.StarExpr:
		return w.expr(x.X, esc), true
	case *ast.ParenExpr:
		return w.expr(x.X, esc), true
	case *ast.SelectorExpr:
		return w.expr(x.X, esc), true
	case *ast.TypeAssertExpr:
		return w.expr(x.X, esc), true
	}
	return false, false
}

func (w *handWalker) indexCompound(x ast.Expr, esc bool) bool {
	switch x := x.(type) {
	case *ast.IndexExpr:
		return w.indexPair(x.X, x.Index, esc)
	case *ast.IndexListExpr:
		if w.expr(x.X, esc) {
			return true
		}
		return w.exprs(x.Indices, esc)
	case *ast.SliceExpr:
		return w.sliceParts(x, esc)
	case *ast.CompositeLit:
		return w.exprs(x.Elts, true)
	case *ast.KeyValueExpr:
		return w.indexPair(x.Key, x.Value, esc)
	}
	return false
}

func (w *handWalker) indexPair(a, b ast.Expr, esc bool) bool {
	if w.expr(a, esc) {
		return true
	}
	return w.expr(b, esc)
}

func (w *handWalker) sliceParts(x *ast.SliceExpr, esc bool) bool {
	if w.expr(x.X, esc) {
		return true
	}
	if w.optExpr(x.Low, esc) {
		return true
	}
	if w.optExpr(x.High, esc) {
		return true
	}
	return w.optExpr(x.Max, esc)
}

func (w *handWalker) stores(lhs []ast.Expr) bool {
	return slices.ContainsFunc(lhs, w.storesOne)
}

func (w *handWalker) storesOne(l ast.Expr) bool {
	switch l := ast.Unparen(l).(type) {
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
		return true
	case *ast.Ident:
		return w.storesGlobal(l)
	}
	return false
}

func (w *handWalker) storesGlobal(id *ast.Ident) bool {
	v, ok := cmp.Or(w.info.Defs[id], w.info.Uses[id]).(*types.Var)
	return ok && (v.Pos() < w.fn.Pos() || v.Pos() >= w.fn.End())
}
