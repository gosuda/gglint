package flow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// Spawn describes one goroutine creation site in a function.
type Spawn struct {
	Site   ast.Node     // *ast.GoStmt, or the Go/TryGo call expression
	Body   *ast.FuncLit // nil when the spawned function is not a literal
	InLoop bool         // Site is inside a for or range body of the enclosing function
	Group  Key          // WaitGroup or errgroup the spawn belongs to; zero when none
}

// Spawns returns the goroutine creation sites of fn in source order.
func Spawns(pass *analysis.Pass, fn ast.Node) []Spawn {
	body := Body(fn)
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return nil
	}
	w := spawnWalker{info: pass.TypesInfo}
	for _, s := range body.List {
		w.stmt(s, false)
	}
	return w.out
}

type spawnWalker struct {
	info *types.Info
	out  []Spawn
}

func (w *spawnWalker) stmt(s ast.Stmt, inLoop bool) {
	switch s := s.(type) {
	case *ast.GoStmt:
		w.addGo(s, inLoop)
	case *ast.BlockStmt:
		w.list(s.List, inLoop)
	case *ast.IfStmt:
		w.optStmt(s.Init, inLoop)
		w.scan(s.Cond, inLoop)
		w.stmt(s.Body, inLoop)
		w.optStmt(s.Else, inLoop)
	case *ast.ForStmt:
		w.optStmt(s.Init, inLoop)
		w.scan(s.Cond, inLoop)
		w.optStmt(s.Post, inLoop)
		w.stmt(s.Body, true)
	case *ast.RangeStmt:
		w.scan(s.X, inLoop)
		w.stmt(s.Body, true)
	case *ast.LabeledStmt:
		w.stmt(s.Stmt, inLoop)
	case *ast.SelectStmt:
		w.stmt(s.Body, inLoop)
	default:
		w.rest(s, inLoop)
	}
}

func (w *spawnWalker) list(l []ast.Stmt, inLoop bool) {
	for _, s := range l {
		w.stmt(s, inLoop)
	}
}

func (w *spawnWalker) rest(s ast.Stmt, inLoop bool) {
	if w.restClause(s, inLoop) {
		return
	}
	w.restExpr(s, inLoop)
}

func (w *spawnWalker) restClause(s ast.Stmt, inLoop bool) bool {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		w.optStmt(s.Init, inLoop)
		w.scan(s.Tag, inLoop)
		w.stmt(s.Body, inLoop)
	case *ast.TypeSwitchStmt:
		w.optStmt(s.Init, inLoop)
		w.stmt(s.Assign, inLoop)
		w.stmt(s.Body, inLoop)
	case *ast.CaseClause:
		w.scanList(s.List, inLoop)
		for _, c := range s.Body {
			w.stmt(c, inLoop)
		}
	case *ast.CommClause:
		w.optStmt(s.Comm, inLoop)
		for _, c := range s.Body {
			w.stmt(c, inLoop)
		}
	default:
		return false
	}
	return true
}

func (w *spawnWalker) restExpr(s ast.Stmt, inLoop bool) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		w.scan(s.X, inLoop)
	case *ast.DeferStmt:
		w.scan(s.Call, inLoop)
	case *ast.AssignStmt:
		w.scanList(s.Lhs, inLoop)
		w.scanList(s.Rhs, inLoop)
	case *ast.ReturnStmt:
		w.scanList(s.Results, inLoop)
	case *ast.SendStmt:
		w.scan(s.Chan, inLoop)
		w.scan(s.Value, inLoop)
	case *ast.DeclStmt:
		w.scanDecl(s, inLoop)
	case *ast.IncDecStmt:
		w.scan(s.X, inLoop)
	}
}

func (w *spawnWalker) optStmt(s ast.Stmt, inLoop bool) {
	if s != nil {
		w.stmt(s, inLoop)
	}
}

func (w *spawnWalker) scan(e ast.Expr, inLoop bool) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			w.check(call, inLoop)
		}
		return true
	})
}

func (w *spawnWalker) scanList(l []ast.Expr, inLoop bool) {
	for _, e := range l {
		w.scan(e, inLoop)
	}
}

func (w *spawnWalker) scanDecl(s *ast.DeclStmt, inLoop bool) {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			w.scanList(vs.Values, inLoop)
		}
	}
}

func (w *spawnWalker) check(call *ast.CallExpr, inLoop bool) {
	callee := typeutil.StaticCallee(w.info, call)
	if callee == nil {
		return
	}
	var key Key
	switch callee.FullName() {
	case "(*sync.WaitGroup).Go", "(*golang.org/x/sync/errgroup.Group).Go", "(*golang.org/x/sync/errgroup.Group).TryGo":
		key, _ = MethodKey(w.info, call)
	default:
		return
	}
	var body *ast.FuncLit
	if len(call.Args) > 0 {
		if lit, ok := ast.Unparen(call.Args[0]).(*ast.FuncLit); ok {
			body = lit
		}
	}
	w.out = append(w.out, Spawn{Site: call, Body: body, InLoop: inLoop, Group: key})
}

func (w *spawnWalker) addGo(s *ast.GoStmt, inLoop bool) {
	var body *ast.FuncLit
	if lit, ok := ast.Unparen(s.Call.Fun).(*ast.FuncLit); ok {
		body = lit
	}
	w.out = append(w.out, Spawn{Site: s, Body: body, InLoop: inLoop, Group: w.goGroup(s, body)})
	w.scanList(s.Call.Args, inLoop)
}

func (w *spawnWalker) goGroup(s *ast.GoStmt, body *ast.FuncLit) Key {
	seen := map[Key]bool{}
	var cands []Key
	add := func(k Key, ok bool) {
		if ok && !seen[k] {
			seen[k] = true
			cands = append(cands, k)
		}
	}
	if body == nil {
		for _, a := range s.Call.Args {
			if isWaitGroup(w.info, a) {
				add(KeyOf(w.info, a))
			}
		}
	} else {
		ast.Inspect(body.Body, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FuncLit, *ast.SelectorExpr:
				return false
			}
			return w.literalCall(s, body, n, add)
		})
	}
	if len(cands) == 1 {
		return cands[0]
	}
	return Key{}
}

func (w *spawnWalker) literalCall(s *ast.GoStmt, body *ast.FuncLit, n ast.Node, add func(Key, bool)) bool {
	if id, ok := n.(*ast.Ident); ok {
		w.literalIdent(s, body, id, add)
		return true
	}
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return true
	}
	return w.literalCallExpr(s, body, call, add)
}

func (w *spawnWalker) literalIdent(s *ast.GoStmt, body *ast.FuncLit, id *ast.Ident, add func(Key, bool)) {
	if !isWaitGroup(w.info, id) {
		return
	}
	k, ok := KeyOf(w.info, id)
	add(w.subst(body, s.Call, k, ok))
}

func (w *spawnWalker) literalCallExpr(s *ast.GoStmt, body *ast.FuncLit, call *ast.CallExpr, add func(Key, bool)) bool {
	callee := typeutil.StaticCallee(w.info, call)
	if callee == nil {
		return true
	}
	if callee.FullName() == "(*sync.WaitGroup).Done" {
		k, ok := MethodKey(w.info, call)
		add(w.subst(body, s.Call, k, ok))
		return true
	}
	if k, ok := MethodKey(w.info, call); ok && w.recvIsGroup(call) {
		add(w.subst(body, s.Call, k, ok))
		return true
	}
	for _, a := range call.Args {
		if isWaitGroup(w.info, a) {
			k, ok := KeyOf(w.info, a)
			add(w.subst(body, s.Call, k, ok))
		}
	}
	return true
}

func (w *spawnWalker) recvIsGroup(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if len(call.Args) > 0 && w.info.Selections[sel] != nil && w.info.Selections[sel].Kind() == types.MethodExpr {
		return isWaitGroup(w.info, call.Args[0])
	}
	return isWaitGroup(w.info, sel.X)
}

func (w *spawnWalker) subst(body *ast.FuncLit, call *ast.CallExpr, k Key, ok bool) (Key, bool) {
	if !ok {
		return Key{}, false
	}
	sig, isSig := w.info.TypeOf(body).(*types.Signature)
	if !isSig || sig == nil {
		return k, true
	}
	for i := range sig.Params().Len() {
		if sig.Params().At(i) != k.Root {
			continue
		}
		param := sig.Params().At(i)
		if _, isPtr := param.Type().Underlying().(*types.Pointer); !isPtr || i >= len(call.Args) {
			return k, true
		}
		argKey, argOK := KeyOf(w.info, call.Args[i])
		if !argOK {
			return Key{}, false
		}
		return Key{Root: argKey.Root, Path: argKey.Path + k.Path}, true
	}
	return k, true
}

func isWaitGroup(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)
	if t == nil {
		return false
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	return n.Obj().Pkg().Path() == "sync" && n.Obj().Name() == "WaitGroup"
}

// Joined reports whether every path from s.Site to at crosses a join of s.
func Joined(pass *analysis.Pass, g *Graph, s Spawn, at ast.Node) bool {
	if pass == nil || pass.TypesInfo == nil || g == nil || s.Site == nil || at == nil {
		return false
	}
	f, t, ok := g.endpoints(s.Site, at)
	if !ok {
		return false
	}
	joins := g.joins(pass, s)
	if joins[t] {
		return true
	}
	return !g.reach(f, t, func(n ast.Node) bool { return joins[n] })
}

func (g *Graph) endpoints(from, to ast.Node) (start, end ast.Node, ok bool) {
	f := g.locate(from)
	t := g.locate(to)
	if f == nil || t == nil || f == t {
		return nil, nil, false
	}
	return f, t, true
}

func (g *Graph) joins(pass *analysis.Pass, s Spawn) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	if !s.Group.zero() {
		g.markWaits(pass.TypesInfo, s.Group, out)
	}
	if s.Body != nil {
		g.markSignals(pass, s.Body, out)
	}
	return out
}

func (g *Graph) markWaits(info *types.Info, group Key, out map[ast.Node]bool) {
	for _, b := range g.blocks {
		for _, n := range b.Nodes {
			if hasWait(info, n, group) {
				out[n] = true
			}
		}
	}
}

func (g *Graph) markSignals(pass *analysis.Pass, body *ast.FuncLit, out map[ast.Node]bool) {
	for _, k := range signaled(pass, body) {
		for _, n := range g.receives(pass.TypesInfo, g.fn, k) {
			out[n] = true
		}
	}
}

func hasWait(info *types.Info, n ast.Node, group Key) bool {
	called := map[*ast.FuncLit]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if call, ok := x.(*ast.CallExpr); ok {
			if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
				called[lit] = true
			}
		}
		return true
	})
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if lit, ok := x.(*ast.FuncLit); ok {
			return called[lit]
		}
		if pruned(x) {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isGroupWait(info, call, group) {
			found = true
			return false
		}
		return true
	})
	return found
}

func pruned(n ast.Node) bool {
	switch n.(type) {
	case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
		return true
	}
	return false
}

func isGroupWait(info *types.Info, call *ast.CallExpr, group Key) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Wait" {
		return false
	}
	k, ok := MethodKey(info, call)
	if !ok || k != group {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	return fn == nil || fn.FullName() == "(*sync.WaitGroup).Wait" || fn.FullName() == "(*golang.org/x/sync/errgroup.Group).Wait"
}

func signaled(pass *analysis.Pass, body *ast.FuncLit) []Key {
	info := pass.TypesInfo
	var out []Key
	seen := map[Key]bool{}
	add := func(k Key, ok bool) {
		if ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	deferredCloses(info, body, add)
	lastClose(info, body, add)
	return out
}

func deferredCloses(info *types.Info, body *ast.FuncLit, add func(Key, bool)) {
	for _, s := range body.Body.List {
		d, ok := s.(*ast.DeferStmt)
		if !ok {
			continue
		}
		add(closeChan(info, d.Call))
		lit, ok := ast.Unparen(d.Call.Fun).(*ast.FuncLit)
		if !ok {
			continue
		}
		ast.Inspect(lit.Body, func(x ast.Node) bool {
			if _, ok := x.(*ast.FuncLit); ok {
				return false
			}
			if call, ok := x.(*ast.CallExpr); ok {
				add(closeChan(info, call))
			}
			return true
		})
	}
}

func lastClose(info *types.Info, body *ast.FuncLit, add func(Key, bool)) {
	n := len(body.Body.List)
	if n == 0 {
		return
	}
	switch s := body.Body.List[n-1].(type) {
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(s.X).(*ast.CallExpr); ok {
			add(closeChan(info, call))
		}
	case *ast.SendStmt:
		add(KeyOf(info, s.Chan))
	}
}

func closeChan(info *types.Info, call *ast.CallExpr) (Key, bool) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != closeFun || len(call.Args) != 1 {
		return Key{}, false
	}
	if b, ok := info.Uses[id].(*types.Builtin); !ok || b.Name() != closeFun {
		return Key{}, false
	}
	return KeyOf(info, call.Args[0])
}

func (g *Graph) receives(info *types.Info, fn ast.Node, k Key) []ast.Node {
	s := recvScan{graph: g, info: info, key: k, called: calledLiterals(fn), skip: map[*ast.UnaryExpr]bool{}}
	s.selects(fn)
	s.walk(fn)
	var out []ast.Node
	for _, r := range s.refined {
		if rn := g.at(r); rn != nil {
			out = append(out, rn)
		}
	}
	return append(out, s.out...)
}

type recvScan struct {
	graph   *Graph
	info    *types.Info
	key     Key
	called  map[*ast.FuncLit]bool
	skip    map[*ast.UnaryExpr]bool
	refined []ast.Node
	out     []ast.Node
}

func (s *recvScan) selects(fn ast.Node) {
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			return n == fn || s.called[lit]
		}
		sel, ok := n.(*ast.SelectStmt)
		if !ok {
			return true
		}
		s.clause(sel)
		return true
	})
}

func (s *recvScan) clause(sel *ast.SelectStmt) {
	for _, cc := range sel.Body.List {
		if c, ok := cc.(*ast.CommClause); ok {
			s.graph.refineClause(s.info, c, s.key, s.skip, &s.refined)
		}
	}
}

func (s *recvScan) walk(fn ast.Node) {
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return n == fn || s.called[n]
		case *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		s.graph.markRecv(s.info, n, s.key, s.skip, &s.out)
		return true
	})
}

func calledLiterals(fn ast.Node) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
		}
		return true
	})
	return out
}

func (g *Graph) at(n ast.Node) ast.Node {
	if r := g.locate(n); r != nil {
		return r
	}
	return g.container(n)
}

func (g *Graph) refineClause(info *types.Info, c *ast.CommClause, k Key, skip map[*ast.UnaryExpr]bool, refined *[]ast.Node) {
	if c.Comm == nil {
		return
	}
	u := recvOf(info, c.Comm, k)
	if u == nil {
		return
	}
	skip[u] = true
	if len(c.Body) > 0 {
		*refined = append(*refined, c.Body[0])
	} else {
		*refined = append(*refined, c.Comm)
	}
}

func (g *Graph) markRecv(info *types.Info, n ast.Node, k Key, skip map[*ast.UnaryExpr]bool, out *[]ast.Node) {
	g.markUnary(info, n, k, skip, out)
	g.markRange(info, n, k, out)
}

func (g *Graph) markUnary(info *types.Info, n ast.Node, k Key, skip map[*ast.UnaryExpr]bool, out *[]ast.Node) {
	u, ok := n.(*ast.UnaryExpr)
	if !ok || u.Op != token.ARROW || skip[u] {
		return
	}
	ck, ok := KeyOf(info, u.X)
	if !ok || ck != k {
		return
	}
	if rn := g.at(u); rn != nil {
		*out = append(*out, rn)
	}
}

func (g *Graph) markRange(info *types.Info, n ast.Node, k Key, out *[]ast.Node) {
	rs, ok := n.(*ast.RangeStmt)
	if !ok {
		return
	}
	ck, ok := KeyOf(info, rs.X)
	if !ok || ck != k {
		return
	}
	if rn := g.at(rs.X); rn != nil {
		*out = append(*out, rn)
	}
}

func recvOf(info *types.Info, comm ast.Stmt, k Key) *ast.UnaryExpr {
	var found *ast.UnaryExpr
	ast.Inspect(comm, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.ARROW {
			return true
		}
		if ck, ok := KeyOf(info, u.X); ok && ck == k {
			found = u
		}
		return false
	})
	return found
}
