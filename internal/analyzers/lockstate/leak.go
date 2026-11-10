package lockstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

type leakCtx struct {
	pass    *analysis.Pass
	graph   *flow.Graph
	before  map[ast.Node]state
	returns []*ast.ReturnStmt
	holders []ast.Node
	tail    bool
	recv    string
}

func runLeak(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkLeak(pass, cur.Node())
	}
	return nil, nil
}

func checkLeak(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	for _, key := range collectKeys(pass.TypesInfo, fn) {
		checkLeakKey(pass, g, fn, key)
	}
}

func checkLeakKey(pass *analysis.Pass, g *flow.Graph, fn ast.Node, key flow.Key) {
	info := pass.TypesInfo
	if hasDeferred(info, fn, key) {
		return
	}
	entry := entryState(info, fn, key)
	tr := func(n ast.Node, s state) state {
		return applyNode(info, key, n, s)
	}
	pre := collectPre(g, entry, tr)
	rets := returnsIn(fn)
	if len(rets) == 0 {
		return
	}
	holders := unlockHolders(pre, info, key)
	if len(holders) == 0 {
		return
	}
	c := &leakCtx{
		pass:    pass,
		graph:   g,
		before:  pre,
		returns: rets,
		holders: holders,
		tail:    lastIsUnlock(info, fn, key),
		recv:    lockRecv(info, fn, key),
	}
	c.report()
}

func (c *leakCtx) report() {
	for _, r := range c.returns {
		c.reportOne(r)
	}
}

func (c *leakCtx) reportOne(r *ast.ReturnStmt) {
	s, ok := c.before[r]
	if !ok {
		return
	}
	if s != writeState && s != readState {
		return
	}
	if !c.good(r) && !c.tail {
		return
	}
	c.pass.Reportf(r.Pos(), "this return leaves %s locked while another return unlocks it, so the next Lock blocks forever: defer the unlock right after the lock", c.recv)
}

func (c *leakCtx) good(skip *ast.ReturnStmt) bool {
	for _, o := range c.returns {
		if o == skip {
			continue
		}
		if c.before[o] != freeState {
			continue
		}
		if c.reached(o) {
			return true
		}
	}
	return false
}

func (c *leakCtx) reached(to ast.Node) bool {
	for _, u := range c.holders {
		if c.graph.Reaches(u, to, nil) {
			return true
		}
	}
	return false
}

func hasDeferred(info *types.Info, fn ast.Node, key flow.Key) bool {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	found := false
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
		k, m, ok := mutexKey(info, ds.Call)
		if ok && k == key && (m == "Unlock" || m == "RUnlock") {
			found = true
			return false
		}
		return true
	})
	return found
}

func returnsIn(fn ast.Node) []*ast.ReturnStmt {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	var out []*ast.ReturnStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		if r, ok := n.(*ast.ReturnStmt); ok {
			out = append(out, r)
		}
		return true
	})
	return out
}

func unlockHolders(pre map[ast.Node]state, info *types.Info, key flow.Key) []ast.Node {
	var out []ast.Node
	for n := range pre {
		if _, ok := n.(*ast.DeferStmt); ok {
			continue
		}
		for _, op := range opsInNode(info, n, key) {
			if op.method == "Unlock" || op.method == "RUnlock" {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

func lastIsUnlock(info *types.Info, fn ast.Node, key flow.Key) bool {
	body := bodyOf(fn)
	if body == nil || len(body.List) == 0 {
		return false
	}
	last := body.List[len(body.List)-1]
	es, ok := last.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	k, m, ok := mutexKey(info, call)
	return ok && k == key && (m == "Unlock" || m == "RUnlock")
}

func bodyOf(fn ast.Node) *ast.BlockStmt {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		return fn.Body
	case *ast.FuncLit:
		return fn.Body
	}
	return nil
}

func lockRecv(info *types.Info, fn ast.Node, key flow.Key) string {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	recv := ""
	ast.Inspect(fn, func(n ast.Node) bool {
		if recv != "" {
			return false
		}
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, m, ok := mutexKey(info, call)
		if !ok || k != key {
			return true
		}
		if m == "Lock" || m == "RLock" || m == "Unlock" || m == "RUnlock" {
			recv = recvText(info, call)
			return false
		}
		return true
	})
	return recv
}
