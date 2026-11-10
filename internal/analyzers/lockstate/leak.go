package lockstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

type leakCtx struct {
	pass        *analysis.Pass
	graph       *flow.Graph
	before      map[ast.Node]state
	returns     []*ast.ReturnStmt
	holders     []ast.Node
	reachedMemo map[ast.Node]bool
	endFree     bool
	recv        string
	info        *types.Info
	key         flow.Key
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
	facts := collectFacts(pass.TypesInfo, fn)
	deferred := deferredKeys(pass.TypesInfo, fn)
	for _, key := range collectKeys(pass.TypesInfo, fn) {
		checkLeakKey(pass, g, fn, key, facts, deferred)
	}
}

func checkLeakKey(pass *analysis.Pass, g *flow.Graph, fn ast.Node, key flow.Key, facts *fnFacts, deferred map[flow.Key]bool) {
	info := pass.TypesInfo
	if facts.storedAddress(key) {
		return
	}
	if deferred[key] {
		return
	}
	if localNeverShared(info, fn, key) {
		return
	}
	entry := facts.entryState(fn, key)
	tr := func(n ast.Node, s state) state {
		if !facts.mentions(n, key.Root) {
			return s
		}
		return applyNode(info, key, n, s)
	}
	pre, order := collectPre(g, entry, tr)
	rets := returnsIn(fn)
	if len(rets) == 0 {
		return
	}
	holders := unlockHolders(order, info, key, facts)
	if len(holders) == 0 {
		return
	}
	c := &leakCtx{
		pass:        pass,
		graph:       g,
		before:      pre,
		returns:     rets,
		holders:     holders,
		reachedMemo: map[ast.Node]bool{},
		endFree:     leakEndFree(info, fn, key, pre),
		recv:        lockRecv(info, fn, key),
		info:        info,
		key:         key,
	}
	c.report()
}

func localNeverShared(info *types.Info, fn ast.Node, key flow.Key) bool {
	v, ok := key.Root.(*types.Var)
	if !ok {
		return false
	}
	return flow.IsLocal(info, fn, v) && !flow.Escapes(info, fn, v)
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
	if s.kind != writeKind && s.kind != readKind {
		return
	}
	if applyNode(c.info, c.key, r, s).kind == unknownKind {
		return
	}
	if !c.good(r) && !c.endFree {
		return
	}
	c.pass.Reportf(r.Pos(), "this return leaves %s locked while another path unlocks it: unlock before returning, or defer the unlock right after the lock", c.recv)
}

func (c *leakCtx) good(skip *ast.ReturnStmt) bool {
	for _, o := range c.returns {
		if o == skip {
			continue
		}
		if c.before[o].kind != freeKind {
			continue
		}
		if c.reached(o) {
			return true
		}
	}
	return false
}

func (c *leakCtx) reached(to ast.Node) bool {
	if hit, ok := c.reachedMemo[to]; ok {
		return hit
	}
	reached := false
	for _, u := range c.holders {
		if c.graph.Reaches(u, to, nil) {
			reached = true
			break
		}
	}
	c.reachedMemo[to] = reached
	return reached
}

func deferredKeys(info *types.Info, fn ast.Node) map[flow.Key]bool {
	keys := map[flow.Key]bool{}
	fnLit := fnLitOf(fn)
	ast.Inspect(fn, func(n ast.Node) bool {
		if skipNestedLit(n, fnLit) {
			return false
		}
		ds, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		deferUnlockKeys(info, ds, keys)
		return true
	})
	return keys
}

func deferUnlockKeys(info *types.Info, ds *ast.DeferStmt, keys map[flow.Key]bool) {
	if k, m, matched := mutexKey(info, ds.Call); matched && isUnlockMethod(m) {
		keys[k] = true
		return
	}
	lit, ok := ast.Unparen(ds.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return
	}
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if k, m, matched := mutexKey(info, call); matched && isUnlockMethod(m) {
			keys[k] = true
		}
		return true
	})
}

func fnLitOf(fn ast.Node) *ast.FuncLit {
	if l, ok := fn.(*ast.FuncLit); ok {
		return l
	}
	return nil
}

func skipNestedLit(n ast.Node, fnLit *ast.FuncLit) bool {
	l, ok := n.(*ast.FuncLit)
	return ok && l != fnLit
}

func isUnlockMethod(m string) bool {
	return m == methodUnlock || m == methodRUnlock
}

func unlockHolders(order []ast.Node, info *types.Info, key flow.Key, facts *fnFacts) []ast.Node {
	var out []ast.Node
	for _, n := range order {
		if _, ok := n.(*ast.DeferStmt); ok {
			continue
		}
		if !facts.mentions(n, key.Root) {
			continue
		}
		for _, op := range opsInNode(info, n, key) {
			if isUnlockMethod(op.method) {
				out = append(out, n)
				break
			}
		}
	}
	return out
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

func leakEndFree(info *types.Info, fn ast.Node, key flow.Key, pre map[ast.Node]state) bool {
	body := flow.Body(fn)
	if body == nil || len(body.List) == 0 {
		return false
	}
	return listEndsFree(info, body.List, key, pre)
}

func listEndsFree(info *types.Info, list []ast.Stmt, key flow.Key, pre map[ast.Node]state) bool {
	if len(list) == 0 {
		return false
	}
	last := list[len(list)-1]
	if isUnlockStmt(info, last, key, pre) {
		return true
	}
	cond, ok := last.(*ast.IfStmt)
	if !ok || cond.Else == nil {
		return false
	}
	return branchEndsFree(info, cond.Body, key, pre) && branchEndsFree(info, cond.Else, key, pre)
}

func branchEndsFree(info *types.Info, n ast.Node, key flow.Key, pre map[ast.Node]state) bool {
	switch n := n.(type) {
	case *ast.BlockStmt:
		return listEndsFree(info, n.List, key, pre)
	case *ast.IfStmt:
		if n.Else == nil {
			return false
		}
		return branchEndsFree(info, n.Body, key, pre) && branchEndsFree(info, n.Else, key, pre)
	}
	return false
}

func isUnlockStmt(info *types.Info, st ast.Stmt, key flow.Key, pre map[ast.Node]state) bool {
	call, ok := unlockCallOf(info, st, key)
	if !ok {
		return false
	}
	_, m, ok := mutexKey(info, call)
	if !ok {
		return false
	}
	if s, ok := pre[st]; ok {
		return stepMethod(m, s).kind == freeKind
	}
	if s, ok := pre[call]; ok {
		return stepMethod(m, s).kind == freeKind
	}
	return false
}

func unlockCallOf(info *types.Info, st ast.Stmt, key flow.Key) (*ast.CallExpr, bool) {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	k, m, ok := mutexKey(info, call)
	if !ok || k != key || !isUnlockMethod(m) {
		return nil, false
	}
	return call, true
}

func lockRecv(info *types.Info, fn ast.Node, key flow.Key) string {
	fnLit := fnLitOf(fn)
	recv := ""
	ast.Inspect(fn, func(n ast.Node) bool {
		if recv != "" {
			return false
		}
		if skipNestedLit(n, fnLit) {
			return false
		}
		if r, ok := lockOpRecv(info, n, key); ok {
			recv = r
			return false
		}
		return true
	})
	return recv
}

func lockOpRecv(info *types.Info, n ast.Node, key flow.Key) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	k, m, ok := mutexKey(info, call)
	if !ok || k != key {
		return "", false
	}
	if !isLockMethod(m) {
		return "", false
	}
	return recvText(info, call), true
}

func isLockMethod(m string) bool {
	return m == methodLock || m == methodRLock
}
