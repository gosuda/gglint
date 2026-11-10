package flow

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

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
	add := w.groupAdder(body, seen, &cands)
	if body == nil {
		w.groupFromArgs(s, add)
	} else {
		w.groupFromBody(s, body, add)
	}
	if len(cands) == 1 {
		return cands[0]
	}
	return Key{}
}

func (w *spawnWalker) groupAdder(body *ast.FuncLit, seen map[Key]bool, cands *[]Key) func(Key, bool) {
	return func(k Key, ok bool) {
		if !ok || seen[k] {
			return
		}
		if body != nil && w.insideLiteral(body, k) {
			return
		}
		seen[k] = true
		*cands = append(*cands, k)
	}
}

func (w *spawnWalker) groupFromArgs(s *ast.GoStmt, add func(Key, bool)) {
	for _, a := range s.Call.Args {
		if isWaitGroup(w.info, a) {
			add(KeyOf(w.info, a))
		}
	}
}

func (w *spawnWalker) groupFromBody(s *ast.GoStmt, body *ast.FuncLit, add func(Key, bool)) {
	deferred := deferredDoneLits(body.Body)
	called := inplaceDoneLits(body.Body)
	ast.Inspect(body.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.GoStmt:
			return false
		case *ast.SelectorExpr:
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			if deferred[lit] || called[lit] {
				return true
			}
			return false
		}
		return w.literalCall(s, body, n, add)
	})
}

func deferredDoneLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if d, ok := n.(*ast.DeferStmt); ok {
			if lit, ok := ast.Unparen(d.Call.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
			return true
		}
		return true
	})
	return out
}

func inplaceDoneLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if _, ok := n.(*ast.DeferStmt); ok {
			return true
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if lit, ok := ast.Unparen(c.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
		}
		return true
	})
	return out
}

func (w *spawnWalker) insideLiteral(body *ast.FuncLit, k Key) bool {
	if k.Root == nil || body.Body == nil {
		return false
	}
	pos := k.Root.Pos()
	return pos >= body.Body.Pos() && pos < body.Body.End()
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
	for _, a := range call.Args {
		if isWaitGroup(w.info, a) {
			k, ok := KeyOf(w.info, a)
			add(w.subst(body, s.Call, k, ok))
		}
	}
	return true
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
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	} else if p, ok := t.Underlying().(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
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
	for _, n := range g.opaqueJoinNodes(pass, s) {
		joins[n] = true
	}
	if joins[t] {
		return true
	}
	return !g.reach(f, t, func(n ast.Node) bool { return joins[n] })
}

func (g *Graph) signaledKeys(pass *analysis.Pass, s Spawn) []Key {
	if s.Body == nil {
		return nil
	}
	return signaled(pass, s.Body)
}

func (g *Graph) collectClosures(pass *analysis.Pass, waits, recvs map[*types.Var]bool, group Key, sigs []Key) {
	info := pass.TypesInfo
	vars := g.cachedClosureVars(info)
	ast.Inspect(g.fn, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		v, ok := vars[lit]
		if !ok || v == nil {
			return true
		}
		if waits != nil && g.litWaits(pass, lit, group) {
			waits[v] = true
		}
		if recvs != nil && g.litRecvs(pass, lit, sigs) {
			recvs[v] = true
		}
		return true
	})
}

func (g *Graph) cachedClosureVars(info *types.Info) map[*ast.FuncLit]*types.Var {
	g.closureOnce.Do(func() {
		m := map[*ast.FuncLit]*types.Var{}
		ast.Inspect(g.fn, func(n ast.Node) bool {
			recordClosureAssign(info, n, m)
			recordClosureValue(info, n, m)
			return true
		})
		g.closureVars = m
	})
	return g.closureVars
}

func recordClosureAssign(info *types.Info, n ast.Node, m map[*ast.FuncLit]*types.Var) {
	s, ok := n.(*ast.AssignStmt)
	if !ok || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}
	lit, ok := ast.Unparen(s.Rhs[0]).(*ast.FuncLit)
	if !ok {
		return
	}
	id, ok := ast.Unparen(s.Lhs[0]).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := info.Defs[id].(*types.Var); ok {
		m[lit] = v
	}
}

func recordClosureValue(info *types.Info, n ast.Node, m map[*ast.FuncLit]*types.Var) {
	s, ok := n.(*ast.ValueSpec)
	if !ok || len(s.Names) != 1 || len(s.Values) != 1 {
		return
	}
	lit, ok := ast.Unparen(s.Values[0]).(*ast.FuncLit)
	if !ok {
		return
	}
	if v, ok := info.Defs[s.Names[0]].(*types.Var); ok {
		m[lit] = v
	}
}

func (g *Graph) litWaits(pass *analysis.Pass, lit *ast.FuncLit, group Key) bool {
	if lit.Body == nil || group.zero() {
		return false
	}
	info := pass.TypesInfo
	called := calledLiterals(lit.Body)
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if f, ok := n.(*ast.FuncLit); ok {
			if _, ok := called[f]; !ok {
				return false
			}
			return true
		}
		return g.litWaitNode(pass, info, n, group, &found)
	})
	return found
}

func (g *Graph) litWaitNode(pass *analysis.Pass, info *types.Info, n ast.Node, group Key, found *bool) bool {
	if c, ok := n.(*ast.CallExpr); ok {
		if isGroupWait(info, c, group) {
			*found = true
			return false
		}
		if g.argsMention(info, c, group) && g.calleeWaits(pass, c) {
			*found = true
			return false
		}
		return true
	}
	if s, ok := n.(*ast.SelectorExpr); ok && s.Sel.Name == waitMethod {
		if k, ok := KeyOf(info, s.X); ok && k == group {
			*found = true
			return false
		}
		return true
	}
	return true
}

func (g *Graph) litRecvs(pass *analysis.Pass, lit *ast.FuncLit, sigs []Key) bool {
	if lit.Body == nil || len(sigs) == 0 {
		return false
	}
	info := pass.TypesInfo
	called := calledLiterals(lit.Body)
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if f, ok := n.(*ast.FuncLit); ok {
			if _, ok := called[f]; !ok {
				return false
			}
			return true
		}
		return g.litRecvNode(pass, info, n, sigs, &found)
	})
	return found
}

func (g *Graph) litRecvNode(pass *analysis.Pass, info *types.Info, n ast.Node, sigs []Key, found *bool) bool {
	if g.checkUnaryRecv(info, n, sigs, found) {
		return !*found
	}
	if g.checkRangeRecv(info, n, sigs, found) {
		return !*found
	}
	if c, ok := n.(*ast.CallExpr); ok {
		if g.mentionsAnySignal(info, c, sigs) && g.calleeRecvs(pass, info, c) {
			*found = true
			return false
		}
		return true
	}
	return true
}

func (g *Graph) checkUnaryRecv(info *types.Info, n ast.Node, sigs []Key, found *bool) bool {
	u, ok := n.(*ast.UnaryExpr)
	if !ok || u.Op != token.ARROW {
		return false
	}
	if k, ok := KeyOf(info, u.X); ok && hasKey(sigs, k) {
		*found = true
		return true
	}
	return true
}

func (g *Graph) checkRangeRecv(info *types.Info, n ast.Node, sigs []Key, found *bool) bool {
	rs, ok := n.(*ast.RangeStmt)
	if !ok {
		return false
	}
	if k, ok := KeyOf(info, rs.X); ok && hasKey(sigs, k) {
		*found = true
		return true
	}
	return true
}

func (g *Graph) mentionsAnySignal(info *types.Info, c *ast.CallExpr, sigs []Key) bool {
	for _, k := range sigs {
		if g.argsMention(info, c, k) {
			return true
		}
	}
	return false
}

func hasKey(keys []Key, k Key) bool {
	return slices.Contains(keys, k)
}

func (g *Graph) callMentionsGroup(info *types.Info, c *ast.CallExpr, group Key, waits map[*types.Var]bool) bool {
	if !group.zero() && g.argsMention(info, c, group) {
		return true
	}
	return g.callsClosure(info, c, waits)
}

func (g *Graph) callMentionsSignal(info *types.Info, c *ast.CallExpr, sigs []Key, recvs map[*types.Var]bool) bool {
	for _, k := range sigs {
		if g.argsMention(info, c, k) {
			return true
		}
	}
	return g.callsClosure(info, c, recvs)
}

func (g *Graph) argsMention(info *types.Info, c *ast.CallExpr, k Key) bool {
	if isGroupWait(info, c, k) {
		return false
	}
	for _, a := range c.Args {
		if ck, ok := KeyOf(info, a); ok && ck == k {
			return true
		}
	}
	return false
}

func (g *Graph) callsClosure(info *types.Info, c *ast.CallExpr, closures map[*types.Var]bool) bool {
	if len(closures) == 0 {
		return false
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	if v, ok := info.Uses[id].(*types.Var); ok && closures[v] {
		return true
	}
	return false
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
	if gs, ok := n.(*ast.GoStmt); ok {
		return argsHaveWait(info, gs.Call, group)
	}
	if ds, ok := n.(*ast.DeferStmt); ok {
		return argsHaveWait(info, ds.Call, group)
	}
	return scanWait(info, n, collectCalled(n), group)
}

func collectCalled(n ast.Node) map[*ast.FuncLit]bool {
	called := map[*ast.FuncLit]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if call, ok := x.(*ast.CallExpr); ok {
			if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
				called[lit] = true
			}
		}
		return true
	})
	return called
}

func scanWait(info *types.Info, n ast.Node, called map[*ast.FuncLit]bool, group Key) bool {
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

func argsHaveWait(info *types.Info, call *ast.CallExpr, group Key) bool {
	if call == nil {
		return false
	}
	for _, a := range call.Args {
		if argHasWait(info, a, group) {
			return true
		}
	}
	return false
}

func argHasWait(info *types.Info, arg ast.Node, group Key) bool {
	hit := false
	ast.Inspect(arg, func(x ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		c, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isGroupWait(info, c, group) {
			hit = true
			return false
		}
		return true
	})
	return hit
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
	if !ok || sel.Sel.Name != waitMethod {
		return false
	}
	k, ok := MethodKey(info, call)
	if !ok || k != group {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	return fn == nil || fn.FullName() == "(*sync.WaitGroup).Wait" || fn.FullName() == "(*golang.org/x/sync/errgroup.Group).Wait"
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
		switch n := n.(type) {
		case *ast.GoStmt:
			markCalledArgs(n.Call, out)
			return false
		case *ast.DeferStmt:
			if lit, ok := ast.Unparen(n.Call.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
			markCalledArgs(n.Call, out)
			return true
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

func markCalledArgs(call *ast.CallExpr, out map[*ast.FuncLit]bool) {
	if call == nil {
		return
	}
	for _, a := range call.Args {
		ast.Inspect(a, func(x ast.Node) bool {
			if c, ok := x.(*ast.CallExpr); ok {
				if lit, ok := ast.Unparen(c.Fun).(*ast.FuncLit); ok {
					out[lit] = true
				}
			}
			return true
		})
	}
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
