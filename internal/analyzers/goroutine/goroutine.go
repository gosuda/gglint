package goroutine

import (
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/inspection"
)

const doc = `goroutine reports goroutines that are not joined or cannot exit.

In standard mode a go statement must launch a function literal that shows
join evidence: defer X.Done() on a sync.WaitGroup, or defer close(ch) or a
send on a channel declared in the enclosing function outside the literal.
In strict mode every go statement is reported in favor of errgroup.Group.Go
or sync.WaitGroup.Go, and such a spawn call inside a loop must follow a
SetLimit call on the same errgroup.Group earlier in the same function.

In every mode a spawned function literal may not contain an infinite loop,
or a loop over time.Tick or a time.Ticker channel, without a return, a break
that leaves the loop, a goto, or a call to panic, os.Exit, runtime.Goexit,
or log.Fatal*, and it may not contain an empty select.`

const (
	msgUnjoined    = "goroutine is not joined: use errgroup.Group.Go or sync.WaitGroup.Go"
	msgBare        = "bare go statement: use errgroup.Group.Go or sync.WaitGroup.Go"
	msgNoExit      = "goroutine loop has no exit path: return when ctx.Done() is closed"
	msgEmptySelect = "goroutine blocks forever on empty select"
	msgFanOut      = "unbounded goroutine fan-out in loop: call SetLimit on the errgroup.Group"
)

const errgroupPath = "golang.org/x/sync/errgroup"

func New(m config.Mode) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     "goroutine",
		Doc:      doc,
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			checker{pass: pass, mode: m}.run()
			return nil, nil
		},
	}
}

type spawner int

const (
	notSpawner spawner = iota
	waitGroupSpawn
	errgroupSpawn
)

type checker struct {
	pass *analysis.Pass
	mode config.Mode
}

func (c checker) run() {
	root := inspection.Of(c.pass).Root()
	for cur := range root.Preorder((*ast.GoStmt)(nil), (*ast.CallExpr)(nil)) {
		switch n := cur.Node().(type) {
		case *ast.GoStmt:
			c.goStmt(cur, n)
		case *ast.CallExpr:
			c.spawnCall(cur, n)
		}
	}
}

func (c checker) goStmt(site inspector.Cursor, stmt *ast.GoStmt) {
	body, ok := spawnedBody(site, stmt.Call.Fun)
	switch {
	case c.mode >= config.Strict:
		c.pass.Reportf(stmt.Pos(), msgBare)
	case !ok || !c.joined(site, body):
		c.pass.Reportf(stmt.Pos(), msgUnjoined)
	}
	if ok {
		c.checkBody(body)
	}
}

func (c checker) spawnCall(site inspector.Cursor, call *ast.CallExpr) {
	kind := c.spawnerOf(call)
	if kind == notSpawner || len(call.Args) != 1 {
		return
	}
	if body, ok := spawnedBody(site, call.Args[0]); ok {
		c.checkBody(body)
	}
	if c.mode >= config.Strict {
		c.checkFanOut(site, call, kind)
	}
}

func (c checker) checkFanOut(site inspector.Cursor, call *ast.CallExpr, kind spawner) {
	if !inLoopBody(site) {
		return
	}
	if kind == errgroupSpawn && c.limitedBefore(site, call) {
		return
	}
	c.pass.Reportf(call.Pos(), msgFanOut)
}

func (c checker) spawnerOf(call *ast.CallExpr) spawner {
	if c.methodOf(call, "sync", "WaitGroup") == "Go" {
		return waitGroupSpawn
	}
	switch c.methodOf(call, errgroupPath, "Group") {
	case "Go", "TryGo":
		return errgroupSpawn
	}
	return notSpawner
}

func (c checker) methodOf(call *ast.CallExpr, pkg, typeName string) string {
	fn, ok := typeutil.Callee(c.pass.TypesInfo, call).(*types.Func)
	if !ok {
		return ""
	}
	recv := fn.Signature().Recv()
	if recv == nil || !isNamed(recv.Type(), pkg, typeName) {
		return ""
	}
	return fn.Name()
}

func spawnedBody(site inspector.Cursor, fun ast.Expr) (inspector.Cursor, bool) {
	lit, ok := ast.Unparen(fun).(*ast.FuncLit)
	if !ok {
		return inspector.Cursor{}, false
	}
	return site.FindNode(lit)
}

type outerScope struct {
	fn, lit ast.Node
}

func (s outerScope) holds(pos token.Pos) bool {
	return spans(s.fn, pos) && !spans(s.lit, pos)
}

func spans(n ast.Node, pos token.Pos) bool {
	return n.Pos() <= pos && pos < n.End()
}

func (c checker) joined(site, body inspector.Cursor) bool {
	fn, ok := enclosing(site, isFunc)
	if !ok {
		return false
	}
	scope := outerScope{fn: fn.Node(), lit: body.Node()}
	for cur := range ownNodes(body, (*ast.DeferStmt)(nil), (*ast.SendStmt)(nil)) {
		if c.joinEvidence(cur.Node(), scope) {
			return true
		}
	}
	return false
}

func (c checker) joinEvidence(n ast.Node, scope outerScope) bool {
	switch n := n.(type) {
	case *ast.DeferStmt:
		return c.waitGroupDone(n.Call) || c.closesOuter(n.Call, scope)
	case *ast.SendStmt:
		return c.outerChan(n.Chan, scope)
	}
	return false
}

func (c checker) waitGroupDone(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return ok && c.methodOf(call, "sync", "WaitGroup") == "Done" &&
		isNamed(c.pass.TypesInfo.TypeOf(sel.X), "sync", "WaitGroup")
}

func (c checker) closesOuter(call *ast.CallExpr, scope outerScope) bool {
	b, ok := typeutil.Callee(c.pass.TypesInfo, call).(*types.Builtin)
	return ok && b.Name() == "close" && len(call.Args) == 1 && c.outerChan(call.Args[0], scope)
}

func (c checker) outerChan(e ast.Expr, scope outerScope) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := c.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return false
	}
	_, isChan := v.Type().Underlying().(*types.Chan)
	return isChan && scope.holds(v.Pos())
}

func (c checker) checkBody(body inspector.Cursor) {
	for cur := range ownNodes(body, (*ast.ForStmt)(nil), (*ast.RangeStmt)(nil), (*ast.SelectStmt)(nil)) {
		c.checkBlocking(cur)
	}
}

func (c checker) checkBlocking(cur inspector.Cursor) {
	switch n := cur.Node().(type) {
	case *ast.ForStmt:
		if n.Cond == nil && !c.leaves(cur, n.Body) {
			c.pass.Reportf(n.Pos(), msgNoExit)
		}
	case *ast.RangeStmt:
		if c.ticks(n.X) && !c.leaves(cur, n.Body) {
			c.pass.Reportf(n.Pos(), msgNoExit)
		}
	case *ast.SelectStmt:
		if len(n.Body.List) == 0 {
			c.pass.Reportf(n.Pos(), msgEmptySelect)
		}
	}
}

func (c checker) ticks(x ast.Expr) bool {
	switch x := ast.Unparen(x).(type) {
	case *ast.CallExpr:
		return isObject(typeutil.Callee(c.pass.TypesInfo, x), "time", "Tick")
	case *ast.SelectorExpr:
		return x.Sel.Name == "C" && isNamed(c.pass.TypesInfo.TypeOf(x.X), "time", "Ticker")
	}
	return false
}

func (c checker) leaves(loop inspector.Cursor, body *ast.BlockStmt) bool {
	bodyCur, ok := loop.FindNode(body)
	if !ok {
		return false
	}
	for cur := range ownNodes(bodyCur, (*ast.ReturnStmt)(nil), (*ast.BranchStmt)(nil), (*ast.CallExpr)(nil)) {
		if c.exits(loop, cur) {
			return true
		}
	}
	return false
}

func (c checker) exits(loop, cur inspector.Cursor) bool {
	switch n := cur.Node().(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return c.branchLeaves(loop, cur, n)
	case *ast.CallExpr:
		return c.terminates(n)
	}
	return false
}

func (c checker) branchLeaves(loop, cur inspector.Cursor, br *ast.BranchStmt) bool {
	switch {
	case br.Tok == token.GOTO:
		return true
	case br.Label != nil:
		return c.jumpsOut(loop, br)
	case br.Tok == token.BREAK:
		target, ok := enclosing(cur, isBreakable)
		return ok && target == loop
	}
	return false
}

func (c checker) jumpsOut(loop inspector.Cursor, br *ast.BranchStmt) bool {
	target := c.pass.TypesInfo.Uses[br.Label]
	if target == nil || target.Pos() >= loop.Node().Pos() {
		return false
	}
	return br.Tok == token.BREAK || target.Pos() != ownLabel(loop)
}

func ownLabel(loop inspector.Cursor) token.Pos {
	if labeled, ok := loop.Parent().Node().(*ast.LabeledStmt); ok {
		return labeled.Label.Pos()
	}
	return token.NoPos
}

func (c checker) terminates(call *ast.CallExpr) bool {
	switch fn := typeutil.Callee(c.pass.TypesInfo, call).(type) {
	case *types.Builtin:
		return fn.Name() == "panic"
	case *types.Func:
		return isObject(fn, "os", "Exit") || isObject(fn, "runtime", "Goexit") || isLogFatal(fn)
	}
	return false
}

func isLogFatal(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "log" && strings.HasPrefix(fn.Name(), "Fatal")
}

func (c checker) limitedBefore(site inspector.Cursor, call *ast.CallExpr) bool {
	fn, ok := enclosing(site, isFunc)
	key := c.receiverKey(call)
	if !ok || key == nil {
		return false
	}
	for cur := range ownNodes(fn, (*ast.CallExpr)(nil)) {
		other := inspection.Node[*ast.CallExpr](cur)
		if other.Pos() < call.Pos() && c.methodOf(other, errgroupPath, "Group") == "SetLimit" &&
			slices.Equal(c.receiverKey(other), key) {
			return true
		}
	}
	return false
}

func (c checker) receiverKey(call *ast.CallExpr) []types.Object {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	return c.objectPath(sel.X)
}

func (c checker) objectPath(e ast.Expr) []types.Object {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if obj := c.pass.TypesInfo.ObjectOf(e); obj != nil {
			return []types.Object{obj}
		}
	case *ast.SelectorExpr:
		if base := c.objectPath(e.X); base != nil {
			return append(base, c.pass.TypesInfo.ObjectOf(e.Sel))
		}
	case *ast.StarExpr:
		return c.objectPath(e.X)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return c.objectPath(e.X)
		}
	}
	return nil
}

func inLoopBody(site inspector.Cursor) bool {
	pos := site.Node().Pos()
	for p := site.Parent(); p.Node() != nil; p = p.Parent() {
		switch n := p.Node().(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return false
		case *ast.ForStmt:
			if spans(n.Body, pos) {
				return true
			}
		case *ast.RangeStmt:
			if spans(n.Body, pos) {
				return true
			}
		}
	}
	return false
}

func enclosing(cur inspector.Cursor, match func(ast.Node) bool) (inspector.Cursor, bool) {
	for p := cur.Parent(); p.Node() != nil; p = p.Parent() {
		if match(p.Node()) {
			return p, true
		}
	}
	return inspector.Cursor{}, false
}

func isFunc(n ast.Node) bool {
	switch n.(type) {
	case *ast.FuncDecl, *ast.FuncLit:
		return true
	}
	return false
}

func isBreakable(n ast.Node) bool {
	switch n.(type) {
	case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func ownNodes(root inspector.Cursor, kinds ...ast.Node) iter.Seq[inspector.Cursor] {
	mask := append([]ast.Node{(*ast.FuncLit)(nil)}, kinds...)
	return func(yield func(inspector.Cursor) bool) {
		more := true
		root.Inspect(mask, func(cur inspector.Cursor) bool {
			if !more {
				return false
			}
			if _, ok := cur.Node().(*ast.FuncLit); ok {
				return cur == root
			}
			more = yield(cur)
			return more
		})
	}
}

func isNamed(t types.Type, pkg, name string) bool {
	if ptr, ok := types.Unalias(t).(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && isObject(named.Obj(), pkg, name)
}

func isObject(obj types.Object, pkg, name string) bool {
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == pkg && obj.Name() == name
}
