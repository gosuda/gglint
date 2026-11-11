package gorace

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
)

var mapAnalyzer = &analysis.Analyzer{
	Name:     "map",
	Doc:      "reports a map a goroutine writes that the function or another goroutine uses without ordering; a loop that waits per iteration is still reported; a sibling goroutine that starts after the first is joined is not paired, and a write bracketed by a send before and a receive after on a channel of capacity one, or of capacity the function cannot see, counts as locked; only index, range, delete, and clear accesses count, and clear counts only when its argument is a map; mutex-passing functions are skipped",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runMap,
}

var fanoutAnalyzer = &analysis.Analyzer{
	Name:     "fanout",
	Doc:      "reports variables declared outside a loop that every goroutine the loop starts writes; indexed writes, guarded writes, and single-goroutine loops are not reported; a loop that waits per iteration is still reported, and a loop that leaves right after the go statement is not; a write between a receive and a send, or a send and a receive, counts as guarded when the function makes the channel with capacity one or its capacity is not visible to the function, and a write between an atomic Load and a Store, or between a receive and a send on a channel not known to be buffered, counts as guarded as well; the check does not verify that only one token exists; mutex-passing functions are skipped",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runFanout,
}

var sharedAnalyzer = &analysis.Analyzer{
	Name:     "shared",
	Doc:      "reports a local variable a goroutine outside a loop reads or writes without ordering; silent when joined, when a common lock is held at both accesses, when both accesses lie between two operations on one channel (the check does not verify a single token), or when a signal and a wait on one channel, WaitGroup, or Cond can order them (the goroutine signals after its access and the parent waits before its own, or the reverse); a send counts as a wait only on a channel that is not known to be buffered; a mutex orders when the parent holds it at the go statement or when it is held at both accesses; a call that is given the object can order and is not reported; sibling goroutines and package or parameter variables are not compared; an access to a whole variable and an access to one of its fields are not paired, and reads of a map are not counted; mutex-passing functions are skipped",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runShared,
}

var doublecheckAnalyzer = &analysis.Analyzer{
	Name:     "doublecheck",
	Doc:      "reports a shared variable or field checked for nil outside a Lock taken before the inner check and assigned under it; locals and RLock are not checked; mutex-passing functions are skipped",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runDoublecheck,
}

var subtestAnalyzer = &analysis.Analyzer{
	Name:     "subtest",
	Doc:      "reports sibling t.Run calls in one block, including the bodies of for and range statements, whose parallel subtests write a variable the other also uses; indexed writes are not checked; a pair of accesses that both hold one mutex declared outside the subtests is not reported; t.Parallel anywhere in the literal counts as parallel",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runSubtest,
}

const (
	atomicPkgPath = "sync/atomic"
	syncPkgPath   = "sync"
)

// Analyzers returns the five gorace analyzers.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{mapAnalyzer, fanoutAnalyzer, sharedAnalyzer, doublecheckAnalyzer, subtestAnalyzer}
}

func keyText(pass *analysis.Pass, k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	base := k.Root.Name()
	if k.Path != "" {
		base += k.Path
	}
	v, ok := k.Root.(*types.Var)
	if !ok || v.Pkg() == nil || pass.Pkg == nil || v.Pkg() == pass.Pkg {
		return base
	}
	return v.Pkg().Name() + "." + base
}

func deref(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	t = types.Unalias(t)
	seen := map[types.Type]bool{}
	for {
		if seen[t] {
			return t
		}
		seen[t] = true
		p, ok := types.Unalias(t).Underlying().(*types.Pointer)
		if !ok {
			return types.Unalias(t)
		}
		t = types.Unalias(p.Elem())
	}
}

func isSyncType(t types.Type) bool {
	if t == nil {
		return false
	}
	n, ok := types.Unalias(deref(t)).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	p := n.Obj().Pkg().Path()
	return p == syncPkgPath || p == atomicPkgPath
}

func isMapType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := deref(t).Underlying().(*types.Map)
	return ok
}

func isMutexNamed(t types.Type) bool {
	n, ok := types.Unalias(deref(t)).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	if n.Obj().Pkg().Path() != syncPkgPath {
		return false
	}
	name := n.Obj().Name()
	return name == "Mutex" || name == "RWMutex"
}

func isMutexVar(v *types.Var) bool {
	if v == nil {
		return false
	}
	return isMutexNamed(v.Type())
}

func outsideLit(o types.Object, lit *ast.FuncLit) bool {
	if o == nil || lit == nil {
		return false
	}
	return o.Pos() < lit.Pos() || o.Pos() >= lit.End()
}

func passesMutex(info *types.Info, fn ast.Node) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callPassesMutex(info, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callPassesMutex(info *types.Info, call *ast.CallExpr) bool {
	for _, a := range call.Args {
		k, ok := flow.KeyOf(info, a)
		if !ok {
			continue
		}
		if v, ok := k.Root.(*types.Var); ok && isMutexVar(v) {
			return true
		}
	}
	return false
}

func isMutexLock(full string) bool {
	return full == "(*sync.Mutex).Lock" || full == "(*sync.RWMutex).Lock"
}

func heldOutside(pass *analysis.Pass, lit *ast.FuncLit, stmt ast.Node) []flow.Key {
	if lit == nil || stmt == nil {
		return nil
	}
	return rootedKeys(newHolder(pass, lit).at(stmt), lit)
}

func lockNodeFor(a varAccess) ast.Node {
	if !a.write {
		return a.stmt
	}
	rs, ok := a.stmt.(*ast.RangeStmt)
	if !ok {
		return a.stmt
	}
	if rs.Body == nil || len(rs.Body.List) == 0 {
		return a.stmt
	}
	return rs.Body.List[0]
}

func rootedKeys(keys []flow.Key, lit *ast.FuncLit) []flow.Key {
	var out []flow.Key
	for _, k := range keys {
		if v, ok := k.Root.(*types.Var); ok && outsideLit(v, lit) {
			out = append(out, k)
		}
	}
	return out
}

type loopNest struct {
	node ast.Node
	body *ast.BlockStmt
}

func collectLoops(fn ast.Node) []loopNest {
	var out []loopNest
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != fn {
			return false
		}
		if body := loopBodyOf(n); body != nil {
			out = append(out, loopNest{node: n, body: body})
		}
		return true
	})
	return out
}

func innermostNest(loops []loopNest, pos token.Pos) (ast.Node, *ast.BlockStmt) {
	var bestLoop ast.Node
	var bestBody *ast.BlockStmt
	for _, l := range loops {
		if !bodyContains(l.body, pos) {
			continue
		}
		if bestBody == nil || l.body.End()-l.body.Pos() < bestBody.End()-bestBody.Pos() {
			bestLoop = l.node
			bestBody = l.body
		}
	}
	return bestLoop, bestBody
}

type guardScope struct {
	pass     *analysis.Pass
	fn       ast.Node
	parents  *holder
	litLocks map[*ast.FuncLit]*holder
	loops    []loopNest
}

func newGuardScope(pass *analysis.Pass, fn ast.Node) *guardScope {
	return &guardScope{pass: pass, fn: fn, parents: newHolder(pass, fn), litLocks: map[*ast.FuncLit]*holder{}, loops: collectLoops(fn)}
}

func (g *guardScope) parentHeld(stmt ast.Node) []flow.Key {
	if stmt == nil {
		return nil
	}
	return g.parents.at(stmt)
}

func (g *guardScope) litHeld(lit *ast.FuncLit, stmt ast.Node) []flow.Key {
	if lit == nil || stmt == nil {
		return nil
	}
	h, ok := g.litLocks[lit]
	if !ok {
		h = newHolder(g.pass, lit)
		g.litLocks[lit] = h
	}
	return rootedKeys(h.at(stmt), lit)
}

func commonKey(a, b []flow.Key) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func funcBodies(pass *analysis.Pass) []ast.Node {
	var decls []ast.Node
	var lits []*ast.FuncLit
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				decls = append(decls, n)
			case *ast.FuncLit:
				lits = append(lits, n)
			}
			return true
		})
	}
	out := make([]ast.Node, 0, len(decls)+len(lits))
	out = append(out, decls...)
	for _, lit := range lits {
		out = append(out, lit)
	}
	return out
}

func declaredOutsideLoop(info *types.Info, loops []loopNest, v types.Object, site ast.Node) bool {
	loop, body := innermostNest(loops, site.Pos())
	if body == nil || loop == nil {
		return true
	}
	if v.Pos() >= body.Pos() && v.Pos() < body.End() {
		return false
	}
	return !headerDefines(info, loop, body, v)
}

func declAvoid(info *types.Info, loops []loopNest, v types.Object, site ast.Node) func(ast.Node) bool {
	_, body := innermostNest(loops, site.Pos())
	if body == nil {
		return nil
	}
	if v == nil || v.Pos() < body.Pos() || v.Pos() >= body.End() {
		return nil
	}
	return func(n ast.Node) bool {
		return definesVar(info, n, v)
	}
}

func definesVar(info *types.Info, n ast.Node, v types.Object) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if id, ok := x.(*ast.Ident); ok && info.Defs[id] == v {
			found = true
			return false
		}
		return true
	})
	return found
}

func loopBodyOf(n ast.Node) *ast.BlockStmt {
	switch n := n.(type) {
	case *ast.ForStmt:
		return n.Body
	case *ast.RangeStmt:
		return n.Body
	}
	return nil
}

func bodyContains(body *ast.BlockStmt, pos token.Pos) bool {
	return body != nil && pos >= body.Pos() && pos < body.End()
}

func exitsListAfter(fn, site ast.Node) bool {
	list, owner := stmtListAfter(fn, site)
	if len(list) == 0 {
		return false
	}
	outer, outerBody := outermostLoop(fn, site.Pos())
	if outer == nil || outerBody == nil {
		return false
	}
	if owner == ast.Node(outerBody) {
		return exitsDirect(list, fn, site.Pos(), outer)
	}
	return exitsNested(list, fn, site.Pos(), outer, owner, outerBody)
}

func outermostLoop(fn ast.Node, pos token.Pos) (ast.Node, *ast.BlockStmt) {
	var bestLoop ast.Node
	var bestBody *ast.BlockStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != fn {
			return false
		}
		body := loopBodyOf(n)
		if !bodyContains(body, pos) {
			return true
		}
		if bestBody == nil || body.End()-body.Pos() > bestBody.End()-bestBody.Pos() {
			bestLoop = n
			bestBody = body
		}
		return true
	})
	return bestLoop, bestBody
}

func exitsDirect(list []ast.Stmt, fn ast.Node, pos token.Pos, outer ast.Node) bool {
	for _, st := range list {
		if isReturnStmt(st) {
			return true
		}
		br, ok := st.(*ast.BranchStmt)
		if !ok || br.Tok != token.BREAK {
			continue
		}
		if br.Label == nil {
			return true
		}
		if breakTargets(br.Label.Name, fn, pos, outer) {
			return true
		}
	}
	return false
}

func exitsNested(list []ast.Stmt, fn ast.Node, pos token.Pos, outer, owner ast.Node, outerBody *ast.BlockStmt) bool {
	return exitsScope{fn: fn, pos: pos, outer: outer, owner: owner, outerBody: outerBody}.exits(list)
}

type exitsScope struct {
	fn        ast.Node
	pos       token.Pos
	outer     ast.Node
	owner     ast.Node
	outerBody *ast.BlockStmt
}

func (s exitsScope) exits(list []ast.Stmt) bool {
	return slices.ContainsFunc(list, s.stmtExits)
}

func (s exitsScope) stmtExits(st ast.Stmt) bool {
	if isReturnStmt(st) {
		return true
	}
	br, ok := st.(*ast.BranchStmt)
	if !ok || br.Tok != token.BREAK {
		return false
	}
	if br.Label == nil {
		return newBreakScope(br, s.owner, s.outerBody).exits()
	}
	return breakTargets(br.Label.Name, s.fn, s.pos, s.outer)
}

type breakScope struct {
	br        *ast.BranchStmt
	owner     ast.Node
	outerBody *ast.BlockStmt
	blocked   bool
}

func newBreakScope(br *ast.BranchStmt, owner ast.Node, outerBody *ast.BlockStmt) *breakScope {
	return &breakScope{br: br, owner: owner, outerBody: outerBody}
}

func (s *breakScope) exits() bool {
	ast.Inspect(s.outerBody, s.visit)
	return !s.blocked
}

func (s *breakScope) visit(n ast.Node) bool {
	if s.blocked {
		return false
	}
	if _, ok := n.(*ast.FuncLit); ok {
		return false
	}
	if n == ast.Node(s.outerBody) || n == s.owner {
		return true
	}
	if isBreakable(n) && n.Pos() <= s.br.Pos() && s.br.Pos() < n.End() {
		s.blocked = true
		return false
	}
	return true
}

func isBreakable(n ast.Node) bool {
	switch n.(type) {
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func breakTargets(name string, fn ast.Node, pos token.Pos, outer ast.Node) bool {
	best := bestLabelTarget(name, fn, pos)
	if best == nil {
		return false
	}
	return best.Pos() <= outer.Pos() && outer.End() <= best.End()
}

func bestLabelTarget(name string, fn ast.Node, pos token.Pos) ast.Node {
	var best ast.Node
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != fn {
			return false
		}
		ls, ok := n.(*ast.LabeledStmt)
		if !ok || ls.Label.Name != name {
			return true
		}
		if pos < ls.Pos() || pos >= ls.End() {
			return true
		}
		if best == nil || ls.End()-ls.Pos() < best.End()-best.Pos() {
			best = ls.Stmt
		}
		return true
	})
	return best
}

func stmtListAfter(fn, site ast.Node) ([]ast.Stmt, ast.Node) {
	if st, ok := site.(ast.Stmt); ok {
		return directStmtListAfter(fn, st)
	}
	return containingStmtListAfter(fn, site)
}

func directStmtListAfter(fn ast.Node, site ast.Stmt) ([]ast.Stmt, ast.Node) {
	var out []ast.Stmt
	var owner ast.Node
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if out != nil {
			return false
		}
		list := stmtListOf(n)
		for i, stmt := range list {
			if stmt == ast.Node(site) {
				out = list[i+1:]
				owner = n
				return false
			}
		}
		return true
	})
	return out, owner
}

func containingStmtListAfter(fn, site ast.Node) ([]ast.Stmt, ast.Node) {
	var best []ast.Stmt
	var owner ast.Node
	bestSize := token.Pos(1 << 30)
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		list := stmtListOf(n)
		for i, stmt := range list {
			size := stmt.End() - stmt.Pos()
			if site.Pos() >= stmt.Pos() && site.Pos() < stmt.End() && size < bestSize {
				best = list[i+1:]
				owner = n
				bestSize = size
			}
		}
		return true
	})
	return best, owner
}

func stmtListOf(n ast.Node) []ast.Stmt {
	switch b := n.(type) {
	case *ast.BlockStmt:
		return b.List
	case *ast.CaseClause:
		return b.Body
	case *ast.CommClause:
		return b.Body
	}
	return nil
}

func isReturnStmt(stmt ast.Stmt) bool {
	_, ok := stmt.(*ast.ReturnStmt)
	return ok
}

func bodyParks(fn ast.Node) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	parks := false
	ast.Inspect(body, func(n ast.Node) bool {
		if parks {
			return false
		}
		switch x := n.(type) {
		case *ast.SendStmt:
			parks = true
			return false
		case *ast.UnaryExpr:
			if x.Op == token.ARROW {
				parks = true
				return false
			}
		case *ast.SelectStmt:
			parks = true
			return false
		case *ast.CallExpr:
			if parkedCall(x) {
				parks = true
				return false
			}
		}
		return true
	})
	return parks
}

func parkedCall(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		id, ok := ast.Unparen(call.Fun).(*ast.Ident)
		return ok && id.Name == "Sleep"
	}
	switch sel.Sel.Name {
	case lockOpLock, lockOpRLock, "Wait", "Sleep", "Recv", "Accept":
		return true
	}
	return false
}

func headerDefines(info *types.Info, loop ast.Node, body *ast.BlockStmt, v types.Object) bool {
	found := false
	ast.Inspect(loop, func(n ast.Node) bool {
		if found || n == ast.Node(body) {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && info.Defs[id] == v {
			found = true
			return false
		}
		return true
	})
	return found
}
