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
	body := bodyOf(fn)
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
	return out
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
	body := bodyOf(fn)
	if body == nil {
		return false
	}
	decl, assigned := false, false
	ast.Inspect(body, func(n ast.Node) bool {
		if assigned {
			return false
		}
		switch n := n.(type) {
		case *ast.ValueSpec:
			decl = decl || specDeclares(info, n, k)
		case *ast.AssignStmt:
			assigned = assigned || (n.Tok == token.ASSIGN && targets(info, n.Lhs, k))
		case *ast.IncDecStmt:
			assigned = assigned || targets(info, []ast.Expr{n.X}, k)
		}
		return !assigned
	})
	return decl && !assigned
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
	body := bodyOf(fn)
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
	c, _ := ast.Unparen(e).(*ast.CallExpr)
	return c, c != nil
}

func makeDef(info *types.Info, fn ast.Node, k flow.Key) (*ast.CallExpr, bool) {
	calls := makeDefs(info, fn, k)
	if len(calls) != 1 {
		return nil, false
	}
	return calls[0], true
}

func madeCap(info *types.Info, fn ast.Node, k flow.Key) (int64, bool) {
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
	body := bodyOf(fn)
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
	body := bodyOf(fn)
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
	body := bodyOf(fn)
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
	body := bodyOf(fn)
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

func loopExits(l []ast.Stmt) bool {
	return exitsList(l, 0)
}

func exitsList(l []ast.Stmt, depth int) bool {
	for _, s := range l {
		if exitsStmt(s, depth) {
			return true
		}
	}
	return false
}

func exitsStmt(s ast.Stmt, depth int) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.GOTO || (s.Tok == token.BREAK && (s.Label != nil || depth == 0))
	case *ast.BlockStmt:
		return exitsList(s.List, depth)
	case *ast.LabeledStmt:
		return exitsStmt(s.Stmt, depth)
	case *ast.IfStmt:
		return exitsOpt(s.Init, depth) || exitsStmt(s.Body, depth) || exitsOpt(s.Else, depth)
	case *ast.ForStmt:
		return exitsOpt(s.Init, depth) || exitsList(s.Body.List, depth+1)
	case *ast.RangeStmt:
		return exitsList(s.Body.List, depth+1)
	default:
		return exitsStructured(s, depth)
	}
}

func exitsStructured(s ast.Stmt, depth int) bool {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		return exitsOpt(s.Init, depth) || exitsCases(s.Body.List, depth+1)
	case *ast.TypeSwitchStmt:
		return exitsOpt(s.Init, depth) || exitsCases(s.Body.List, depth+1)
	case *ast.SelectStmt:
		return exitsCases(s.Body.List, depth+1)
	case *ast.CaseClause:
		return exitsList(s.Body, depth)
	case *ast.CommClause:
		return exitsList(s.Body, depth)
	}
	return false
}

func exitsCases(l []ast.Stmt, depth int) bool {
	return exitsList(l, depth)
}

func exitsOpt(s ast.Stmt, depth int) bool {
	return s != nil && exitsStmt(s, depth)
}

func spawnLits(info *types.Info, fn ast.Node) map[*ast.FuncLit]bool {
	body := bodyOf(fn)
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

func handsOn(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := bodyOf(fn)
	if body == nil {
		return false
	}
	w := &handWalker{info: info, key: k, fn: fn, spawned: spawnLits(info, fn)}
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
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return w.exprs(s.Results, true)
	case *ast.GoStmt:
		return w.expr(s.Call, true)
	case *ast.DeferStmt:
		return w.expr(s.Call, true)
	case *ast.SendStmt:
		return w.expr(s.Value, true)
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

func (w *handWalker) structured(s ast.Stmt, esc bool) bool {
	switch s := s.(type) {
	case *ast.BlockStmt:
		return w.list(s.List, esc)
	case *ast.LabeledStmt:
		return w.stmt(s.Stmt, esc)
	case *ast.IfStmt:
		return w.opt(s.Init, esc) || w.optExpr(s.Cond, esc) || w.list(s.Body.List, esc) || w.opt(s.Else, esc)
	case *ast.ForStmt:
		return w.opt(s.Init, esc) || w.optExpr(s.Cond, esc) || w.opt(s.Post, esc) || w.list(s.Body.List, esc)
	case *ast.RangeStmt:
		return w.optExpr(s.X, esc) || w.list(s.Body.List, esc)
	case *ast.SwitchStmt:
		return w.opt(s.Init, esc) || w.optExpr(s.Tag, esc) || w.clauses(s.Body.List, esc)
	case *ast.TypeSwitchStmt:
		return w.opt(s.Init, esc) || w.stmt(s.Assign, esc) || w.clauses(s.Body.List, esc)
	case *ast.SelectStmt:
		return w.clauses(s.Body.List, esc)
	default:
		return w.clause(s, esc)
	}
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
	return w.exprs(s.Rhs, rhs) || w.exprs(s.Lhs, false)
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
		return esc && w.match(x)
	case *ast.FuncLit:
		return w.literal(x, esc)
	case *ast.UnaryExpr:
		return w.unary(x, esc)
	case *ast.CallExpr:
		return w.call(x, esc)
	case *ast.BinaryExpr:
		return w.expr(x.X, esc) || w.expr(x.Y, esc)
	default:
		return w.compound(x, esc)
	}
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
	switch x := x.(type) {
	case *ast.StarExpr:
		return w.expr(x.X, esc)
	case *ast.ParenExpr:
		return w.expr(x.X, esc)
	case *ast.SelectorExpr:
		return w.expr(x.X, esc)
	case *ast.TypeAssertExpr:
		return w.expr(x.X, esc)
	case *ast.IndexExpr:
		return w.expr(x.X, esc) || w.expr(x.Index, esc)
	case *ast.IndexListExpr:
		return w.expr(x.X, esc) || w.exprs(x.Indices, esc)
	case *ast.SliceExpr:
		return w.expr(x.X, esc) || w.optExpr(x.Low, esc) || w.optExpr(x.High, esc) || w.optExpr(x.Max, esc)
	case *ast.CompositeLit:
		return w.exprs(x.Elts, esc)
	case *ast.KeyValueExpr:
		return w.expr(x.Key, esc) || w.expr(x.Value, esc)
	}
	return false
}

func (w *handWalker) stores(lhs []ast.Expr) bool {
	for _, l := range lhs {
		if w.storesOne(l) {
			return true
		}
	}
	return false
}

func (w *handWalker) storesOne(l ast.Expr) bool {
	switch l := ast.Unparen(l).(type) {
	case *ast.SelectorExpr, *ast.IndexExpr:
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

func loneSend(info *types.Info, lit *ast.FuncLit) (chanOp, bool) {
	if lit == nil {
		return chanOp{}, false
	}
	w := &opWalker{info: info}
	w.stmts(lit.Body.List)
	var send chanOp
	found := false
	for _, op := range w.out {
		if op.Kind != opSend || op.InSelect {
			continue
		}
		if found {
			return chanOp{}, false
		}
		send, found = op, true
	}
	return send, found
}

func loneSelectRecv(info *types.Info, fn ast.Node, k flow.Key) (*ast.SelectStmt, bool) {
	body := bodyOf(fn)
	if body == nil {
		return nil, false
	}
	w := &opWalker{info: info, key: k}
	w.stmts(body.List)
	var recv chanOp
	found := false
	for _, op := range w.out {
		if op.Kind != opRecv {
			continue
		}
		if found {
			return nil, false
		}
		recv, found = op, true
	}
	if !found || recv.Sel == nil {
		return nil, false
	}
	return recv.Sel, true
}

func otherReturns(sel *ast.SelectStmt, recv ast.Node) bool {
	for _, s := range sel.Body.List {
		cc, ok := s.(*ast.CommClause)
		if !ok || insideComm(cc, recv) {
			continue
		}
		if flow.EndsInReturn(cc.Body) {
			return true
		}
	}
	return false
}

func insideComm(cc *ast.CommClause, n ast.Node) bool {
	return cc.Comm != nil && cc.Comm.Pos() <= n.Pos() && n.End() <= cc.Comm.End()
}

func straightRecvs(info *types.Info, fn ast.Node, loop ast.Stmt, k flow.Key) int64 {
	body := bodyOf(fn)
	if body == nil {
		return 0
	}
	start := sliceIndex(body.List, loop)
	if start < 0 {
		return 0
	}
	var count int64
	for _, s := range body.List[start+1:] {
		if !flow.StraightLine(fn, loop, s) {
			continue
		}
		count += recvCount(info, s, k)
	}
	return count
}

func recvCount(info *types.Info, s ast.Stmt, k flow.Key) int64 {
	w := &opWalker{info: info, key: k}
	w.stmt(s)
	var count int64
	for _, op := range w.out {
		if op.Kind == opRecv && !op.InSelect {
			count++
		}
	}
	return count
}

func countLoops(fn ast.Node) []ast.Stmt {
	body := bodyOf(fn)
	if body == nil {
		return nil
	}
	var out []ast.Stmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.ForStmt:
			out = append(out, n)
		case *ast.RangeStmt:
			out = append(out, n)
		}
		return true
	})
	return out
}

func directGos(l []ast.Stmt) []*ast.GoStmt {
	var out []*ast.GoStmt
	for _, s := range l {
		if g, ok := s.(*ast.GoStmt); ok {
			out = append(out, g)
		}
	}
	return out
}

func loopTrip(info *types.Info, st ast.Stmt) (int64, bool) {
	switch st := st.(type) {
	case *ast.ForStmt:
		return forTrip(info, st)
	case *ast.RangeStmt:
		return constInt(info, st.X)
	}
	return 0, false
}

func forTrip(info *types.Info, st *ast.ForStmt) (int64, bool) {
	def, ok := st.Init.(*ast.AssignStmt)
	if !ok || def.Tok != token.DEFINE || len(def.Lhs) != 1 || len(def.Rhs) != 1 {
		return 0, false
	}
	id, ok := def.Lhs[0].(*ast.Ident)
	if !ok || !zeroInit(info, def.Rhs[0]) || !incPost(info, st.Post, id) {
		return 0, false
	}
	cond, ok := st.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LSS || !loopsIdent(info, cond.X, id) {
		return 0, false
	}
	return constInt(info, cond.Y)
}

func zeroInit(info *types.Info, e ast.Expr) bool {
	n, ok := constInt(info, e)
	return ok && n == 0
}

func incPost(info *types.Info, post ast.Stmt, id *ast.Ident) bool {
	inc, ok := post.(*ast.IncDecStmt)
	if !ok || inc.Tok != token.INC {
		return false
	}
	return loopsIdent(info, inc.X, id)
}

func loopsIdent(info *types.Info, n ast.Node, id *ast.Ident) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if u, ok := x.(*ast.Ident); ok && info.Uses[u] == info.Defs[id] {
			found = true
		}
		return !found
	})
	return found
}

func sliceIndex(l []ast.Stmt, s ast.Stmt) int {
	for i, c := range l {
		if c == s {
			return i
		}
	}
	return -1
}

func underSelect(fn ast.Node, target ast.Node) bool {
	found := false
	ast.Inspect(fn, func(x ast.Node) bool {
		if found {
			return false
		}
		sel, ok := x.(*ast.SelectStmt)
		if !ok {
			return true
		}
		for _, c := range sel.Body.List {
			cc, ok := c.(*ast.CommClause)
			if !ok || cc.Comm == nil {
				continue
			}
			if withinNode(cc.Comm, target) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func withinNode(outer, inner ast.Node) bool {
	return outer.Pos() <= inner.Pos() && inner.End() <= outer.End()
}
