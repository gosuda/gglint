package flow

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// Joined reports whether every path from s.Site to at crosses a join of s.
func Joined(pass *analysis.Pass, g *Graph, s Spawn, at ast.Node) bool {
	if pass == nil || pass.TypesInfo == nil || g == nil || s.Site == nil || at == nil {
		return false
	}
	f, t, ok := g.endpoints(s.Site, at)
	if !ok {
		return false
	}
	joins := g.cachedJoins(pass, s)
	if joins[t] {
		return true
	}
	return !g.spanReach(pass, s, f)[t]
}

func (g *Graph) spanReach(pass *analysis.Pass, s Spawn, start ast.Node) map[ast.Node]bool {
	g.joinMu.Lock()
	if m, ok := g.spanMemo[s.Site]; ok {
		g.joinMu.Unlock()
		return m
	}
	g.joinMu.Unlock()
	joins := g.cachedJoins(pass, s)
	m := g.flood(start, func(n ast.Node) bool { return joins[n] })
	g.joinMu.Lock()
	if g.spanMemo == nil {
		g.spanMemo = map[ast.Node]map[ast.Node]bool{}
	}
	g.spanMemo[s.Site] = m
	g.joinMu.Unlock()
	return m
}

func (g *Graph) cachedJoins(pass *analysis.Pass, s Spawn) map[ast.Node]bool {
	g.joinMu.Lock()
	if g.joinMemo != nil {
		if m, ok := g.joinMemo[s.Site]; ok {
			g.joinMu.Unlock()
			return m
		}
	}
	g.joinMu.Unlock()
	m := g.joins(pass, s)
	for _, n := range g.opaqueJoinNodes(pass, s) {
		m[n] = true
	}
	g.joinMu.Lock()
	if g.joinMemo == nil {
		g.joinMemo = map[ast.Node]map[ast.Node]bool{}
	}
	g.joinMemo[s.Site] = m
	g.joinMu.Unlock()
	return m
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
		if _, ok := n.(*ast.DeferStmt); ok {
			return false
		}
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
