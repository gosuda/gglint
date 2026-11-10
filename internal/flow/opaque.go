package flow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

func (g *Graph) opaqueJoinNodes(pass *analysis.Pass, s Spawn) []ast.Node {
	info := pass.TypesInfo
	if s.Group.zero() && s.Body == nil {
		return nil
	}
	sigs := g.signaledKeys(pass, s)
	waits := map[*types.Var]bool{}
	recvs := map[*types.Var]bool{}
	if !s.Group.zero() || len(sigs) != 0 {
		g.collectClosures(info, waits, recvs, s.Group, sigs)
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
	skip := goDeferCalls(o.graph.fn)
	ast.Inspect(o.graph.fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != o.graph.fn {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if skip[c] || o.graph.nested(c) {
			return true
		}
		if !o.reports(info, c) {
			return true
		}
		if rn := o.graph.at(c); rn != nil {
			out = append(out, rn)
		}
		return true
	})
	return out
}

func goDeferCalls(fn ast.Node) map[*ast.CallExpr]bool {
	out := map[*ast.CallExpr]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		switch s := n.(type) {
		case *ast.GoStmt:
			out[s.Call] = true
		case *ast.DeferStmt:
			out[s.Call] = true
		}
		return true
	})
	return out
}

func (o opaqueScan) reports(info *types.Info, c *ast.CallExpr) bool {
	if isJoinExempt(info, c) {
		return false
	}
	if o.graph.callMentionsGroup(info, c, o.spawn.Group, o.waits) {
		if !o.graph.trivialCallee(o.pass, info, c) {
			return true
		}
	}
	if o.graph.callMentionsSignal(info, c, o.sigs, o.recvs) {
		if !o.graph.trivialCallee(o.pass, info, c) {
			return true
		}
	}
	return false
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
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Wait" {
			found = true
			return false
		}
		return true
	})
	return found
}

func bodyHasRecv(info *types.Info, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
			found = true
			return false
		}
		if rs, ok := n.(*ast.RangeStmt); ok && isChanType(info.TypeOf(rs.X)) {
			found = true
			return false
		}
		return true
	})
	return found
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
