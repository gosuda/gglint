package chanstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func chanLocals(info *types.Info, fn ast.Node) []flow.Key {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []flow.Key
	seen := map[flow.Key]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := info.Defs[id].(*types.Var)
		if !ok || !isChan(v.Type()) || !flow.IsLocal(info, fn, v) {
			return true
		}
		k := flow.Key{Root: v}
		if seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return keepLocalWith(out, info, fn, spawnLits(info, fn))
}

func keepLocalWith(out []flow.Key, info *types.Info, fn ast.Node, spawned map[*ast.FuncLit]bool) []flow.Key {
	var kept []flow.Key
	for _, k := range out {
		if handsOnWith(info, fn, k, spawned) {
			continue
		}
		kept = append(kept, k)
	}
	return kept
}

func nilLocals(info *types.Info, fn ast.Node) []flow.Key {
	var out []flow.Key
	for _, k := range chanLocals(info, fn) {
		if nilVar(info, fn, k) {
			out = append(out, k)
		}
	}
	return out
}

func nilVar(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	seen := &nilSeen{}
	ast.Inspect(body, func(n ast.Node) bool {
		if seen.assigned {
			return false
		}
		seen.visit(info, n, k)
		return !seen.assigned
	})
	return seen.decl && !seen.assigned
}

type nilSeen struct {
	decl     bool
	assigned bool
}

func (s *nilSeen) visit(info *types.Info, n ast.Node, k flow.Key) {
	switch n := n.(type) {
	case *ast.ValueSpec:
		s.decl = s.decl || specDeclares(info, n, k)
	case *ast.AssignStmt:
		s.assignStmt(info, n, k)
	case *ast.IncDecStmt:
		s.assigned = s.assigned || targets(info, []ast.Expr{n.X}, k)
	case *ast.RangeStmt:
		s.assigned = s.assigned || rangeTargets(info, n, k)
	}
}

func (s *nilSeen) assignStmt(info *types.Info, n *ast.AssignStmt, k flow.Key) {
	s.assigned = s.assigned || targets(info, n.Lhs, k)
}

func rangeTargets(info *types.Info, r *ast.RangeStmt, k flow.Key) bool {
	if r.Tok != token.ASSIGN && r.Tok != token.DEFINE {
		return false
	}
	if r.Key != nil && targets(info, []ast.Expr{r.Key}, k) {
		return true
	}
	return r.Value != nil && targets(info, []ast.Expr{r.Value}, k)
}

func specDeclares(info *types.Info, vs *ast.ValueSpec, k flow.Key) bool {
	if len(vs.Values) != 0 {
		return false
	}
	return targets(info, identExprs(vs.Names), k)
}

func identExprs(ids []*ast.Ident) []ast.Expr {
	out := make([]ast.Expr, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func targets(info *types.Info, lhs []ast.Expr, k flow.Key) bool {
	for _, l := range lhs {
		if kk, ok := keyOfIdent(info, l); ok && kk == k {
			return true
		}
	}
	return false
}

func makeDefs(info *types.Info, fn ast.Node, k flow.Key) []*ast.CallExpr {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		collectMakes(info, n, k, &out)
		return true
	})
	return out
}

func collectMakes(info *types.Info, n ast.Node, k flow.Key, out *[]*ast.CallExpr) {
	switch a := n.(type) {
	case *ast.AssignStmt:
		defMakes(info, a, k, out)
	case *ast.ValueSpec:
		specMakes(info, a, k, out)
	}
}

func defMakes(info *types.Info, a *ast.AssignStmt, k flow.Key, out *[]*ast.CallExpr) {
	if a.Tok != token.DEFINE || len(a.Rhs) != len(a.Lhs) {
		return
	}
	for i, l := range a.Lhs {
		if kk, ok := keyOfIdent(info, l); !ok || kk != k {
			continue
		}
		if c, ok := makeCall(info, a.Rhs[i]); ok {
			*out = append(*out, c)
		}
	}
}

func specMakes(info *types.Info, vs *ast.ValueSpec, k flow.Key, out *[]*ast.CallExpr) {
	if len(vs.Values) != 1 {
		return
	}
	if !targets(info, identExprs(vs.Names), k) {
		return
	}
	if c, ok := makeCall(info, vs.Values[0]); ok {
		*out = append(*out, c)
	}
}

func makeCall(info *types.Info, e ast.Expr) (*ast.CallExpr, bool) {
	if !isMake(info, e) {
		return nil, false
	}
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	return c, true
}

func makeDef(info *types.Info, fn ast.Node, k flow.Key) (*ast.CallExpr, bool) {
	calls := makeDefs(info, fn, k)
	if len(calls) != 1 {
		return nil, false
	}
	return calls[0], true
}

func reassigned(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	c := keyAssignCounter{info: info, key: k}
	ast.Inspect(body, func(n ast.Node) bool {
		if c.count > 1 {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		c.countNode(n)
		return c.count <= 1
	})
	return c.count > 1
}

type keyAssignCounter struct {
	info  *types.Info
	key   flow.Key
	count int
}

func (c *keyAssignCounter) countNode(n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if targets(c.info, n.Lhs, c.key) {
			c.count++
		}
	case *ast.ValueSpec:
		if len(n.Values) > 0 && targets(c.info, identExprs(n.Names), c.key) {
			c.count++
		}
	case *ast.RangeStmt:
		if n.Tok == token.ASSIGN && rangeTargets(c.info, n, c.key) {
			c.count++
		}
	}
}

func madeCap(info *types.Info, fn ast.Node, k flow.Key) (int64, bool) {
	if reassigned(info, fn, k) {
		return 0, false
	}
	call, ok := makeDef(info, fn, k)
	if !ok {
		return 0, false
	}
	return constCap(info, call)
}

func unbuffered(info *types.Info, fn ast.Node, k flow.Key) bool {
	c, ok := madeCap(info, fn, k)
	return ok && c == 0
}

func channelCopied(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	copied := false
	ast.Inspect(body, func(n ast.Node) bool {
		if copied {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			copied = copiesKey(info, n.Lhs, n.Rhs, k)
		case *ast.ValueSpec:
			copied = copiesKey(info, identExprs(n.Names), n.Values, k)
		}
		return !copied
	})
	return copied
}

func copiesKey(info *types.Info, lhs, rhs []ast.Expr, k flow.Key) bool {
	if !mentionsChan(rhs, info, k) {
		return false
	}
	for _, l := range lhs {
		if kk, ok := keyOfIdent(info, l); ok && kk != k {
			return true
		}
	}
	return false
}

func mentionsChan(rhs []ast.Expr, info *types.Info, k flow.Key) bool {
	for _, e := range rhs {
		if kk, ok := keyOfIdent(info, ast.Unparen(e)); ok && kk == k {
			return true
		}
	}
	return false
}

func localKey(info *types.Info, fn ast.Node, k flow.Key) bool {
	v, ok := k.Root.(*types.Var)
	return ok && flow.IsLocal(info, fn, v)
}

func loopBody(st ast.Stmt) []ast.Stmt {
	switch st := st.(type) {
	case *ast.ForStmt:
		return st.Body.List
	case *ast.RangeStmt:
		return st.Body.List
	}
	return nil
}

func chanRanges(info *types.Info, fn ast.Node) []*ast.RangeStmt {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []*ast.RangeStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if r, ok := n.(*ast.RangeStmt); ok && isChan(info.TypeOf(r.X)) {
			out = append(out, r)
		}
		return true
	})
	return out
}

func hasRange(info *types.Info, fn ast.Node, k flow.Key) bool {
	for _, r := range chanRanges(info, fn) {
		if kk, ok := flow.KeyOf(info, r.X); ok && kk == k {
			return true
		}
	}
	return false
}

func closesAnywhere(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if ck, ok := closeKey(info, c); ok && ck == k {
				found = true
			}
		}
		return !found
	})
	return found
}

func literalMentions(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			found = mentionsKey(info, lit, k)
			return false
		}
		return true
	})
	return found
}

func literalTouch(info *types.Info, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := x.(*ast.FuncLit); ok {
			found = mentionsKey(info, lit, k)
			return false
		}
		return true
	})
	return found
}

func mentionsKey(info *types.Info, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if id, ok := x.(*ast.Ident); ok {
			if kk, ok := keyOfIdent(info, id); ok && kk == k {
				found = true
			}
		}
		return !found
	})
	return found
}

func chanOps(info *types.Info, fn ast.Node, k flow.Key, want opKind) []chanOp {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	w := &opWalker{info: info, key: k}
	w.stmts(body.List)
	var out []chanOp
	for _, op := range w.out {
		if op.Kind&want != 0 && !op.InSelect {
			out = append(out, op)
		}
	}
	slices.SortStableFunc(out, func(a, b chanOp) int { return cmp.Compare(a.Pos, b.Pos) })
	return out
}

func deadPair(g *flow.Graph, ops []chanOp) (chanOp, bool) {
	for i := range ops {
		for j := i + 1; j < len(ops); j++ {
			if ops[i].Kind == ops[j].Kind {
				continue
			}
			if !g.Reaches(ops[i].Expr, ops[j].Expr, nil) {
				continue
			}
			return ops[i], true
		}
	}
	return chanOp{}, false
}

func spawnLits(info *types.Info, fn ast.Node) map[*ast.FuncLit]bool {
	body := flow.Body(fn)
	out := map[*ast.FuncLit]bool{}
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			if lit, ok := ast.Unparen(n.Call.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
		case *ast.CallExpr:
			markGroupSpawn(info, n, out)
		}
		return true
	})
	return out
}

func spawnDrainsWith(info *types.Info, spawned map[*ast.FuncLit]bool, k flow.Key) bool {
	for lit := range spawned {
		if litDrains(info, lit, k) {
			return true
		}
	}
	return false
}

func litDrains(info *types.Info, lit *ast.FuncLit, k flow.Key) bool {
	drains := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if drains {
			return false
		}
		drains = drainsAt(info, n, k)
		return !drains
	})
	return drains
}

func drainsAt(info *types.Info, n ast.Node, k flow.Key) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.ARROW && keyMatches(info, n.X, k)
	case *ast.RangeStmt:
		return isChan(info.TypeOf(n.X)) && keyMatches(info, n.X, k)
	}
	return false
}

func keyMatches(info *types.Info, e ast.Expr, k flow.Key) bool {
	kk, ok := flow.KeyOf(info, e)
	return ok && kk == k
}

func markGroupSpawn(info *types.Info, call *ast.CallExpr, out map[*ast.FuncLit]bool) {
	callee := typeutil.StaticCallee(info, call)
	if callee == nil || len(call.Args) == 0 {
		return
	}
	switch callee.FullName() {
	case "(*sync.WaitGroup).Go", "(*golang.org/x/sync/errgroup.Group).Go", "(*golang.org/x/sync/errgroup.Group).TryGo":
		if lit, ok := ast.Unparen(call.Args[0]).(*ast.FuncLit); ok {
			out[lit] = true
		}
	}
}

func handsOnWith(info *types.Info, fn ast.Node, k flow.Key, spawned map[*ast.FuncLit]bool) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	w := &handWalker{info: info, key: k, fn: fn, spawned: spawned}
	return w.list(body.List, false)
}

func handsOn(info *types.Info, fn ast.Node, k flow.Key) bool {
	return handsOnWith(info, fn, k, spawnLits(info, fn))
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

func spawnExits(info *types.Info, fn ast.Node, k flow.Key) bool {
	for lit := range spawnLits(info, fn) {
		if !mentionsKey(info, lit, k) {
			continue
		}
		if litExits(info, lit) {
			return true
		}
	}
	return false
}

func litExits(info *types.Info, lit *ast.FuncLit) bool {
	exited := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if exited {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if s, ok := n.(*ast.ExprStmt); ok {
			if c, ok := s.X.(*ast.CallExpr); ok && isNeverReturn(info, c) {
				exited = true
			}
		}
		return !exited
	})
	return exited
}

func isNeverReturn(info *types.Info, c *ast.CallExpr) bool {
	if isPanicCall(info, c) {
		return true
	}
	fn := typeutil.StaticCallee(info, c)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() + "." + fn.Name() {
	case "os.Exit", "log.Fatal", "log.Fatalf", "log.Fatalln":
		return true
	}
	return false
}
