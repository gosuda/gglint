package lockstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

type helperCache struct {
	info     *types.Info
	pkg      *types.Package
	decls    map[*types.Func]ast.Node
	memo     map[*types.Func]bool
	pureMemo map[*types.Func]bool
}

func newHelperCache(pass *analysis.Pass) *helperCache {
	c := &helperCache{
		info:     pass.TypesInfo,
		pkg:      pass.Pkg,
		decls:    map[*types.Func]ast.Node{},
		memo:     map[*types.Func]bool{},
		pureMemo: map[*types.Func]bool{},
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				return true
			}
			if fn, ok := c.info.Defs[fd.Name].(*types.Func); ok {
				if _, exists := c.decls[fn]; !exists {
					c.decls[fn] = fd.Body
				}
			}
			return true
		})
	}
	return c
}

func (c *helperCache) unlocks(fn *types.Func) bool {
	if v, ok := c.memo[fn]; ok {
		return v
	}
	body, ok := c.bodyOf(fn)
	if !ok {
		c.memo[fn] = false
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if m := methodName(c.info, call); m == methodUnlock || m == methodRUnlock {
			found = true
			return false
		}
		return true
	})
	c.memo[fn] = found
	return found
}

func (c *helperCache) bodyOf(fn *types.Func) (ast.Node, bool) {
	if body, ok := c.decls[fn]; ok {
		return body, true
	}
	if o := fn.Origin(); o != fn {
		body, ok := c.decls[o]
		return body, ok
	}
	return nil, false
}

func (c *helperCache) staticLocal(call *ast.CallExpr) (*types.Func, bool) {
	fn := typeutil.StaticCallee(c.info, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != c.pkg {
		return nil, false
	}
	if _, ok := c.decls[fn]; ok {
		return fn, true
	}
	if o := fn.Origin(); o != fn {
		if _, ok := c.decls[o]; ok {
			return o, true
		}
	}
	return nil, false
}

func (c *helperCache) passesRoot(call *ast.CallExpr, key flow.Key) bool {
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if k, ok := flow.KeyOf(c.info, sel.X); ok && k.Root == key.Root {
			return true
		}
	}
	for _, a := range call.Args {
		if k, ok := flow.KeyOf(c.info, a); ok && k.Root == key.Root {
			return true
		}
	}
	return false
}

func (c *helperCache) isPure(call *ast.CallExpr, key flow.Key) bool {
	fn, ok := c.staticLocal(call)
	if !ok || !c.passesRoot(call, key) {
		return false
	}
	return c.pureBody(fn)
}

func (c *helperCache) pureBody(fn *types.Func) bool {
	if v, ok := c.pureMemo[fn]; ok {
		return v
	}
	body, ok := c.bodyOf(fn)
	if !ok {
		c.pureMemo[fn] = false
		return false
	}
	pure := true
	ast.Inspect(body, func(n ast.Node) bool {
		if !pure {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if c.trivialCall(call) {
			return true
		}
		pure = false
		return false
	})
	c.pureMemo[fn] = pure
	return pure
}

func (c *helperCache) trivialCall(call *ast.CallExpr) bool {
	if _, ok := flow.IsCall(c.info, call); !ok {
		return true
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if _, ok := c.info.Uses[id].(*types.Builtin); ok {
			return true
		}
	}
	return false
}

func (c *helperCache) isUnlocking(call *ast.CallExpr, key flow.Key) bool {
	fn, ok := c.staticLocal(call)
	if !ok || !c.passesRoot(call, key) {
		return false
	}
	return c.unlocks(fn)
}

func (c *helperCache) hasDeferredRelease(fn ast.Node, key flow.Key) bool {
	found := false
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		ds, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if c.deferReleases(ds, key) {
			found = true
			return false
		}
		return true
	})
	return found
}

func (c *helperCache) deferReleases(ds *ast.DeferStmt, key flow.Key) bool {
	if c.isUnlocking(ds.Call, key) {
		return true
	}
	lit, ok := ast.Unparen(ds.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return false
	}
	return c.litUnlocks(lit, key)
}

func (c *helperCache) litUnlocks(lit *ast.FuncLit, key flow.Key) bool {
	found := false
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if found {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if c.isUnlocking(call, key) {
			found = true
			return false
		}
		return true
	})
	return found
}
