package flow

import (
	"go/ast"
	"go/token"
	"go/types"
)

// EndsInReturn reports whether the last statement of list is a return.
func EndsInReturn(list []ast.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	_, ok := list[len(list)-1].(*ast.ReturnStmt)
	return ok
}

// StraightLine reports whether from and to are elements of one statement list of fn with no control, spawn, or labeled statement between them.
func StraightLine(fn ast.Node, from, to ast.Stmt) bool {
	if fn == nil || from == nil || to == nil {
		return false
	}
	return scanLists(fn, from, to)
}

func scanLists(fn ast.Node, from, to ast.Stmt) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		switch n := n.(type) {
		case *ast.BlockStmt:
			found = between(n.List, from, to)
		case *ast.CaseClause:
			found = between(n.Body, from, to)
		case *ast.CommClause:
			found = between(n.Body, from, to)
		default:
			return true
		}
		return !found
	})
	return found
}

func between(list []ast.Stmt, from, to ast.Stmt) bool {
	fi, ti := listIndex(list, from), listIndex(list, to)
	if fi < 0 || ti < 0 || fi == ti {
		return false
	}
	if fi > ti {
		fi, ti = ti, fi
	}
	return !anyBreak(list[fi+1 : ti])
}

func listIndex(list []ast.Stmt, s ast.Stmt) int {
	for i, c := range list {
		if c == s {
			return i
		}
	}
	return -1
}

func anyBreak(list []ast.Stmt) bool {
	for _, s := range list {
		if breaksLine(s) {
			return true
		}
	}
	return false
}

func breaksLine(s ast.Stmt) bool {
	switch s.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		return true
	case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	case *ast.GoStmt, *ast.DeferStmt, *ast.LabeledStmt:
		return true
	}
	return false
}

// IsCall reports whether e is a call that is neither a conversion nor a call of len or cap.
func IsCall(info *types.Info, e ast.Expr) (*ast.CallExpr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	if isLenCap(info, call) {
		return nil, false
	}
	if info != nil && info.Types[call.Fun].IsType() {
		return nil, false
	}
	return call, true
}

func isLenCap(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || !isLenCapName(id.Name) {
		return false
	}
	if info == nil {
		return true
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && isLenCapName(b.Name())
}

func isLenCapName(name string) bool {
	switch name {
	case "len", "cap":
		return true
	}
	return false
}

// IsLocal reports whether v is declared in the body of fn rather than as a parameter, result, or receiver.
func IsLocal(info *types.Info, fn ast.Node, v *types.Var) bool {
	if info == nil || fn == nil || v == nil {
		return false
	}
	ft, recv := funcSig(fn)
	if ft == nil {
		return false
	}
	if definesParam(info, ft, recv, v) {
		return false
	}
	body := fnBody(fn)
	if body == nil {
		return false
	}
	return findLocalDef(info, fn, body, v)
}

func definesParam(info *types.Info, ft *ast.FuncType, recv *ast.FieldList, v *types.Var) bool {
	for _, fields := range []*ast.FieldList{ft.Params, ft.Results, recv} {
		if fieldDefines(info, fields, v) {
			return true
		}
	}
	return false
}

func findLocalDef(info *types.Info, fn ast.Node, body *ast.BlockStmt, v *types.Var) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || info.Defs[id] != v {
			return true
		}
		found = true
		return false
	})
	return found
}

func funcSig(fn ast.Node) (*ast.FuncType, *ast.FieldList) {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		if fn.Type == nil {
			return nil, nil
		}
		return fn.Type, fn.Recv
	case *ast.FuncLit:
		return fn.Type, nil
	}
	return nil, nil
}

func fieldDefines(info *types.Info, fields *ast.FieldList, v *types.Var) bool {
	if fields == nil {
		return false
	}
	for _, f := range fields.List {
		for _, id := range f.Names {
			if info.Defs[id] == v {
				return true
			}
		}
	}
	return false
}

// DefinedBy reports the expression e when v is declared by v := e with no later assignment to v in fn.
func DefinedBy(info *types.Info, fn ast.Node, v *types.Var) (ast.Expr, bool) {
	body := fnBody(fn)
	if info == nil || body == nil || v == nil {
		return nil, false
	}
	def, rhs := findDef(info, fn, body, v)
	if def == nil {
		return nil, false
	}
	if assignedLater(info, fn, v, def.Pos()) {
		return nil, false
	}
	return rhs, true
}

func findDef(info *types.Info, fn ast.Node, body *ast.BlockStmt, v *types.Var) (ast.Stmt, ast.Expr) {
	f := defFinder{info: info, fn: fn, target: v}
	ast.Inspect(body, f.node)
	return f.def, f.rhs
}

type defFinder struct {
	info   *types.Info
	fn     ast.Node
	target *types.Var
	def    ast.Stmt
	rhs    ast.Expr
}

func (f *defFinder) node(n ast.Node) bool {
	if f.def != nil {
		return false
	}
	if lit, ok := n.(*ast.FuncLit); ok && lit != f.fn {
		return false
	}
	def, rhs, ok := asDef(f.info, n, f.target)
	if !ok {
		return true
	}
	f.def, f.rhs = def, rhs
	return false
}

func asDef(info *types.Info, n ast.Node, v *types.Var) (ast.Stmt, ast.Expr, bool) {
	a, ok := n.(*ast.AssignStmt)
	if !ok || a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return nil, nil, false
	}
	if !defines(info, a.Lhs[0], v) {
		return nil, nil, false
	}
	return a, a.Rhs[0], true
}

func defines(info *types.Info, e ast.Expr, v *types.Var) bool {
	id, ok := e.(*ast.Ident)
	return ok && info.Defs[id] == v
}

func assignedLater(info *types.Info, fn ast.Node, v *types.Var, after token.Pos) bool {
	found := false
	ast.Inspect(fnBody(fn), func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Pos() > after && assigns(info, n.Lhs, v) {
				found = true
			}
		case *ast.IncDecStmt:
			if n.Pos() > after && targetIs(info, n.X, v) {
				found = true
			}
		}
		return !found
	})
	return found
}

func assigns(info *types.Info, lhs []ast.Expr, v *types.Var) bool {
	for _, l := range lhs {
		if targetIs(info, l, v) {
			return true
		}
	}
	return false
}

func targetIs(info *types.Info, e ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	return info.Uses[id] == v || info.Defs[id] == v
}

// MethodKey returns the key of the receiver of a method call, following any promotion path.
func MethodKey(info *types.Info, call *ast.CallExpr) (Key, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || info == nil {
		return Key{}, false
	}
	s := info.Selections[sel]
	if s == nil {
		return Key{}, false
	}
	recv := sel.X
	if s.Kind() == types.MethodExpr {
		if len(call.Args) == 0 {
			return Key{}, false
		}
		recv = call.Args[0]
	} else if s.Kind() != types.MethodVal {
		return Key{}, false
	}
	base, ok := KeyOf(info, recv)
	if !ok {
		return Key{}, false
	}
	return extendPromotion(base, s)
}

func extendPromotion(base Key, s *types.Selection) (Key, bool) {
	t := s.Recv()
	idx := s.Index()
	for _, i := range idx[:len(idx)-1] {
		st, ok := derefType(t).Underlying().(*types.Struct)
		if !ok || i >= st.NumFields() {
			return Key{}, false
		}
		base.Path += "." + st.Field(i).Name()
		t = st.Field(i).Type()
	}
	return base, true
}

func derefType(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// Escapes reports whether v may still be reachable after fn returns.
func Escapes(info *types.Info, fn ast.Node, v *types.Var) bool {
	if info == nil || fn == nil || v == nil {
		return false
	}
	body := fnBody(fn)
	if body == nil {
		return false
	}
	e := escaper{info: info, target: v, fn: fn}
	return e.list(body.List, false)
}

type escaper struct {
	info   *types.Info
	target *types.Var
	fn     ast.Node
}

func (e *escaper) list(l []ast.Stmt, esc bool) bool {
	for _, s := range l {
		if e.stmt(s, esc) {
			return true
		}
	}
	return false
}

func (e *escaper) exprs(l []ast.Expr, esc bool) bool {
	for _, x := range l {
		if e.expr(x, esc) {
			return true
		}
	}
	return false
}

func (e *escaper) optStmt(s ast.Stmt, esc bool) bool {
	return s != nil && e.stmt(s, esc)
}

func (e *escaper) optExpr(x ast.Expr, esc bool) bool {
	return x != nil && e.expr(x, esc)
}

func (e *escaper) stmt(s ast.Stmt, esc bool) bool {
	switch s := s.(type) {
	case *ast.BadStmt, *ast.EmptyStmt, *ast.BranchStmt:
		return false
	case *ast.ReturnStmt:
		return e.exprs(s.Results, true)
	case *ast.GoStmt:
		return e.expr(s.Call, true)
	case *ast.DeferStmt:
		return e.expr(s.Call, true)
	case *ast.SendStmt:
		return e.send(s, esc)
	case *ast.ExprStmt:
		return e.expr(s.X, esc)
	case *ast.IncDecStmt:
		return e.expr(s.X, esc)
	default:
		return e.structured(s, esc)
	}
}

func (e *escaper) send(s *ast.SendStmt, esc bool) bool {
	return e.expr(s.Chan, esc) || e.expr(s.Value, true)
}

func (e *escaper) structured(s ast.Stmt, esc bool) bool {
	switch s := s.(type) {
	case *ast.AssignStmt:
		return e.assign(s, esc)
	case *ast.DeclStmt:
		return e.decl(s, esc)
	case *ast.BlockStmt:
		return e.list(s.List, esc)
	case *ast.LabeledStmt:
		return e.stmt(s.Stmt, esc)
	case *ast.IfStmt:
		return e.flowIf(s, esc)
	case *ast.ForStmt:
		return e.flowFor(s, esc)
	case *ast.RangeStmt:
		return e.flowRange(s, esc)
	default:
		return e.selectish(s, esc)
	}
}

func (e *escaper) selectish(s ast.Stmt, esc bool) bool {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		return e.switchStmt(s, esc)
	case *ast.TypeSwitchStmt:
		return e.typeSwitch(s, esc)
	case *ast.CaseClause:
		return e.exprs(s.List, esc) || e.list(s.Body, esc)
	case *ast.CommClause:
		return e.optStmt(s.Comm, esc) || e.list(s.Body, esc)
	case *ast.SelectStmt:
		return e.stmt(s.Body, esc)
	}
	return false
}

func (e *escaper) switchStmt(s *ast.SwitchStmt, esc bool) bool {
	return e.optStmt(s.Init, esc) || e.optExpr(s.Tag, esc) || e.stmt(s.Body, esc)
}

func (e *escaper) typeSwitch(s *ast.TypeSwitchStmt, esc bool) bool {
	return e.optStmt(s.Init, esc) || e.stmt(s.Assign, esc) || e.stmt(s.Body, esc)
}

func (e *escaper) flowIf(s *ast.IfStmt, esc bool) bool {
	return e.optStmt(s.Init, esc) || e.optExpr(s.Cond, esc) || e.stmt(s.Body, esc) || e.optStmt(s.Else, esc)
}

func (e *escaper) flowFor(s *ast.ForStmt, esc bool) bool {
	return e.optStmt(s.Init, esc) || e.optExpr(s.Cond, esc) || e.optStmt(s.Post, esc) || e.stmt(s.Body, esc)
}

func (e *escaper) flowRange(s *ast.RangeStmt, esc bool) bool {
	return e.optExpr(s.X, esc) || e.optExpr(s.Key, false) || e.optExpr(s.Value, false) || e.stmt(s.Body, esc)
}

func (e *escaper) assign(s *ast.AssignStmt, esc bool) bool {
	rhs := esc || e.store(s.Lhs)
	return e.exprs(s.Rhs, rhs) || e.exprs(s.Lhs, false)
}

func (e *escaper) store(lhs []ast.Expr) bool {
	for _, l := range lhs {
		switch l := ast.Unparen(l).(type) {
		case *ast.SelectorExpr, *ast.IndexExpr:
			return true
		case *ast.Ident:
			if vv, ok := e.defOrUse(l).(*types.Var); ok && e.global(vv) {
				return true
			}
		}
	}
	return false
}

func (e *escaper) defOrUse(id *ast.Ident) types.Object {
	if obj := e.info.Defs[id]; obj != nil {
		return obj
	}
	return e.info.Uses[id]
}

func (e *escaper) global(v *types.Var) bool {
	p := v.Pos()
	return p < e.fn.Pos() || p >= e.fn.End()
}

func (e *escaper) decl(s *ast.DeclStmt, esc bool) bool {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if e.exprs(vs.Values, esc) {
			return true
		}
	}
	return false
}

func (e *escaper) expr(x ast.Expr, esc bool) bool {
	if x == nil {
		return false
	}
	if id, ok := x.(*ast.Ident); ok {
		return esc && e.info.Uses[id] == e.target
	}
	if c := singleChild(x); c != nil {
		return e.expr(c, esc)
	}
	switch x := x.(type) {
	case *ast.FuncLit:
		return e.list(x.Body.List, true)
	case *ast.CallExpr:
		return e.call(x, esc)
	case *ast.UnaryExpr:
		return e.unary(x, esc)
	default:
		return e.compound(x, esc)
	}
}

func (e *escaper) unary(x *ast.UnaryExpr, esc bool) bool {
	if x.Op == token.AND {
		return e.expr(x.X, true)
	}
	return e.expr(x.X, esc)
}

func singleChild(x ast.Expr) ast.Expr {
	switch x := x.(type) {
	case *ast.StarExpr:
		return x.X
	case *ast.ParenExpr:
		return x.X
	case *ast.SelectorExpr:
		return x.X
	case *ast.TypeAssertExpr:
		return x.X
	}
	return nil
}

func (e *escaper) compound(x ast.Expr, esc bool) bool {
	switch x := x.(type) {
	case *ast.BinaryExpr:
		return e.expr(x.X, esc) || e.expr(x.Y, esc)
	case *ast.CompositeLit:
		return e.exprs(x.Elts, esc)
	case *ast.IndexExpr:
		return e.expr(x.X, esc) || e.expr(x.Index, esc)
	case *ast.KeyValueExpr:
		return e.expr(x.Key, esc) || e.expr(x.Value, esc)
	default:
		return e.indexRest(x, esc)
	}
}

func (e *escaper) indexRest(x ast.Expr, esc bool) bool {
	switch x := x.(type) {
	case *ast.IndexListExpr:
		return e.expr(x.X, esc) || e.exprs(x.Indices, esc)
	case *ast.SliceExpr:
		return e.optExpr(x.X, esc) || e.optExpr(x.Low, esc) || e.optExpr(x.High, esc) || e.optExpr(x.Max, esc)
	}
	return false
}

func (e *escaper) call(c *ast.CallExpr, esc bool) bool {
	plain := e.plain(c)
	args := !plain
	if lit, ok := ast.Unparen(c.Fun).(*ast.FuncLit); ok && !plain {
		if e.list(lit.Body.List, esc) {
			return true
		}
	} else if e.expr(c.Fun, false) {
		return true
	}
	return e.exprs(c.Args, args)
}

func (e *escaper) plain(c *ast.CallExpr) bool {
	if e.info.Types[c.Fun].IsType() {
		return true
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok || !isLenCapName(id.Name) {
		return false
	}
	b, ok := e.info.Uses[id].(*types.Builtin)
	return ok && isLenCapName(b.Name())
}
