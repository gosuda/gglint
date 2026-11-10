package flow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

const waitMethod = "Wait"

func (g *Graph) opaqueJoinNodes(pass *analysis.Pass, s Spawn) []ast.Node {
	info := pass.TypesInfo
	if s.Group.zero() && s.Body == nil {
		return nil
	}
	sigs := g.signaledKeys(pass, s)
	waits := map[*types.Var]bool{}
	recvs := map[*types.Var]bool{}
	if !s.Group.zero() || len(sigs) != 0 {
		g.collectClosures(pass, waits, recvs, s.Group, sigs)
	}
	join := opaqueScan{graph: g, pass: pass, spawn: s, sigs: sigs, waits: waits, recvs: recvs}
	return join.collect(info)
}

type opaqueScan struct {
	graph *Graph
	pass  *analysis.Pass
	spawn Spawn
	sigs  []Key
	waits map[*types.Var]bool
	recvs map[*types.Var]bool
}

func (o opaqueScan) collect(info *types.Info) []ast.Node {
	var out []ast.Node
	scan := collectScan{scan: o, info: info, skip: goDeferCalls(o.graph.fn)}
	scan.called = calledLiterals(o.graph.fn)
	scan.closures = o.graph.cachedClosureVars(info)
	ast.Inspect(o.graph.fn, func(n ast.Node) bool {
		return scan.visit(n, &out)
	})
	return out
}

type collectScan struct {
	scan     opaqueScan
	info     *types.Info
	skip     map[*ast.CallExpr]bool
	called   map[*ast.FuncLit]bool
	closures map[*ast.FuncLit]*types.Var
}

func (s collectScan) visit(n ast.Node, out *[]ast.Node) bool {
	switch n.(type) {
	case *ast.GoStmt, *ast.DeferStmt:
		return false
	}
	if lit, ok := n.(*ast.FuncLit); ok && lit != s.scan.graph.fn {
		_, isCalled := s.called[lit]
		_, isClosure := s.closures[lit]
		if !isCalled && !isClosure {
			return false
		}
		return true
	}
	c, ok := n.(*ast.CallExpr)
	if !ok {
		return true
	}
	return s.appendCall(c, out)
}

func (s collectScan) appendCall(c *ast.CallExpr, out *[]ast.Node) bool {
	if s.skip[c] || s.scan.graph.nested(c) {
		return true
	}
	if !s.scan.reports(s.info, c) {
		return true
	}
	if rn := s.scan.graph.at(c); rn != nil {
		*out = append(*out, rn)
	}
	return true
}

func goDeferCalls(fn ast.Node) map[*ast.CallExpr]bool {
	out := map[*ast.CallExpr]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.GoStmt:
			out[s.Call] = true
			return false
		case *ast.DeferStmt:
			out[s.Call] = true
			return false
		}
		return true
	})
	return out
}

func (o opaqueScan) reports(info *types.Info, c *ast.CallExpr) bool {
	if isJoinExempt(info, c) {
		return false
	}
	if o.iifeJoins(c) {
		return true
	}
	if o.groupJoins(info, c) {
		return true
	}
	return o.signalJoins(info, c)
}

func (o opaqueScan) iifeJoins(c *ast.CallExpr) bool {
	lit, ok := ast.Unparen(c.Fun).(*ast.FuncLit)
	if !ok {
		return false
	}
	if !o.spawn.Group.zero() && o.graph.litWaits(o.pass, lit, o.spawn.Group) {
		return true
	}
	if len(o.sigs) != 0 && o.graph.litRecvs(o.pass, lit, o.sigs) {
		return true
	}
	return false
}

func (o opaqueScan) groupJoins(info *types.Info, c *ast.CallExpr) bool {
	if !o.graph.callMentionsGroup(info, c, o.spawn.Group, o.waits) {
		return false
	}
	return !o.graph.trivialCallee(o.pass, info, c)
}

func (o opaqueScan) signalJoins(info *types.Info, c *ast.CallExpr) bool {
	if !o.graph.callMentionsSignal(info, c, o.sigs, o.recvs) {
		return false
	}
	return !o.graph.trivialCallee(o.pass, info, c)
}

func isJoinExempt(info *types.Info, c *ast.CallExpr) bool {
	if info.Types[c.Fun].IsType() {
		return true
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = info.Uses[id].(*types.Builtin)
	return ok
}

func (g *Graph) trivialCallee(pass *analysis.Pass, info *types.Info, c *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, c)
	if fn == nil {
		return false
	}
	decl := findFuncDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	if bodyHasWait(decl.Body) {
		return false
	}
	if bodyHasRecv(info, decl.Body) {
		return false
	}
	return !bodyForwardsParam(info, decl)
}

func findFuncDecl(pass *analysis.Pass, fn *types.Func) *ast.FuncDecl {
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if pass.TypesInfo.Defs[fd.Name] == fn {
				return fd
			}
		}
	}
	return nil
}

func bodyHasWait(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	scan := waitScan{called: calledLiterals(body)}
	ast.Inspect(body, scan.visit)
	return scan.found
}

type waitScan struct {
	called map[*ast.FuncLit]bool
	found  bool
}

func (s *waitScan) visit(n ast.Node) bool {
	if s.found {
		return false
	}
	if _, ok := n.(*ast.GoStmt); ok {
		return false
	}
	if lit, ok := n.(*ast.FuncLit); ok {
		if _, ok := s.called[lit]; !ok {
			return false
		}
		return true
	}
	return s.checkWait(n)
}

func (s *waitScan) checkWait(n ast.Node) bool {
	if c, ok := n.(*ast.CallExpr); ok {
		if sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == waitMethod {
			s.found = true
			return false
		}
		return true
	}
	if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == waitMethod {
		s.found = true
		return false
	}
	return true
}

func bodyHasRecv(info *types.Info, body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	scan := recvScanBody{called: calledLiterals(body), info: info}
	ast.Inspect(body, scan.visit)
	return scan.found
}

type recvScanBody struct {
	called map[*ast.FuncLit]bool
	info   *types.Info
	found  bool
}

func (s *recvScanBody) visit(n ast.Node) bool {
	if s.found {
		return false
	}
	if _, ok := n.(*ast.GoStmt); ok {
		return false
	}
	if lit, ok := n.(*ast.FuncLit); ok {
		if _, ok := s.called[lit]; !ok {
			return false
		}
		return true
	}
	return s.checkRecv(n)
}

func (s *recvScanBody) checkRecv(n ast.Node) bool {
	if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
		s.found = true
		return false
	}
	if rs, ok := n.(*ast.RangeStmt); ok && isChanType(s.info.TypeOf(rs.X)) {
		s.found = true
		return false
	}
	return true
}

func (g *Graph) calleeWaits(pass *analysis.Pass, c *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, c)
	if fn == nil {
		return false
	}
	decl := findFuncDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	return bodyHasWait(decl.Body)
}

func (g *Graph) calleeRecvs(pass *analysis.Pass, info *types.Info, c *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, c)
	if fn == nil {
		return false
	}
	decl := findFuncDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	return bodyHasRecv(info, decl.Body)
}

func isChanType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func bodyForwardsParam(info *types.Info, decl *ast.FuncDecl) bool {
	params := paramVars(info, decl)
	if len(params) == 0 {
		return false
	}
	found := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callUsesParam(info, c, params) {
			found = true
			return false
		}
		return true
	})
	return found
}

func paramVars(info *types.Info, decl *ast.FuncDecl) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	if decl.Type.Params == nil {
		return out
	}
	for _, f := range decl.Type.Params.List {
		for _, id := range f.Names {
			if v, ok := info.Defs[id].(*types.Var); ok {
				out[v] = true
			}
		}
	}
	return out
}

func callUsesParam(info *types.Info, c *ast.CallExpr, params map[*types.Var]bool) bool {
	for _, a := range c.Args {
		hit := false
		ast.Inspect(a, func(n ast.Node) bool {
			if hit {
				return false
			}
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if v, ok := info.Uses[id].(*types.Var); ok && params[v] {
				hit = true
				return false
			}
			return true
		})
		if hit {
			return true
		}
	}
	return false
}
