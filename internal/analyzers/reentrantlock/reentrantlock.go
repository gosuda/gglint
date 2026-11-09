package reentrantlock

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	relockFormat = "%s is locked again while already held: sync mutexes are not reentrant, so this deadlocks"
	callFormat   = "call to %s locks %s while it is already held: sync mutexes are not reentrant, so this deadlocks"
)

var Analyzer = &analysis.Analyzer{
	Name: "reentrant_withlock_deadlock",
	Doc: `report locking a sync mutex that the same goroutine already holds

A Lock statement on a sync.Mutex or sync.RWMutex holds the mutex over the
statements after it in the same statement list, up to the first statement
that unlocks the same receiver anywhere outside a defer, go statement, or
function literal, or to the end of the list, as with a deferred Unlock.
Inside that region, a Lock on the same receiver blocks forever, and so does
a call to a same-package function whose synchronous execution locks it at
any depth; mutually recursive helpers reach a fixed point. A same-package
function that calls one of its function parameters while it holds a mutex
runs the function literal, named function, or method value passed there
under that mutex, with a method value's receiver bound to the call-site
operand. A go statement evaluates its function operand and arguments
synchronously and runs the call in a new goroutine. A defer statement
evaluates its function operand and arguments synchronously and runs the call
at function exit; within a held region that reaches the end of its statement
list, the call runs under the mutex unless a later deferred Unlock of the
same mutex, written directly in that region, runs first. A function literal that
is neither invoked directly nor passed to a parameter its callee invokes is
skipped. Two receivers match when they name the same variable through the
same chain of field selections, ignoring address-of and dereference; any
other receiver never matches. A parameter that is not a pointer receives a
copy, so a mutex reached through it never matches.`,
	Run: run,
}

type heldCall struct {
	key   lockKey
	param int
}

type checker struct {
	pass      *analysis.Pass
	order     []*types.Func
	decls     map[*types.Func]*ast.FuncDecl
	invokes   map[*types.Func]map[int]bool
	heldCalls map[*types.Func][]heldCall
	summaries map[*types.Func]map[lockKey]bool
	limit     int
	reported  map[*ast.CallExpr]bool
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{
		pass:      pass,
		decls:     make(map[*types.Func]*ast.FuncDecl),
		summaries: make(map[*types.Func]map[lockKey]bool),
		reported:  make(map[*ast.CallExpr]bool),
	}
	c.index()
	c.invokes = c.findInvokes()
	c.heldCalls = c.findHeldCalls()
	c.summarize()
	for _, fn := range c.order {
		c.check(c.decls[fn])
	}
	return nil, nil
}

func (c *checker) index() {
	for _, f := range c.pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fn, ok := c.pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
				c.decls[fn] = fd
				c.order = append(c.order, fn)
			}
		}
	}
}

func (c *checker) callee(call *ast.CallExpr) *types.Func {
	fn := typeutil.StaticCallee(c.pass.TypesInfo, call)
	if fn == nil {
		return nil
	}
	fn = fn.Origin()
	if _, ok := c.decls[fn]; !ok {
		return nil
	}
	return fn
}

func (c *checker) paramIndex(sig *types.Signature, fun ast.Expr) int {
	id, ok := ast.Unparen(fun).(*ast.Ident)
	if !ok {
		return -1
	}
	obj := c.pass.TypesInfo.Uses[id]
	for i := range sig.Params().Len() {
		if sig.Params().At(i) == obj {
			return i
		}
	}
	return -1
}

func (c *checker) findInvokes() map[*types.Func]map[int]bool {
	found := make(map[*types.Func]map[int]bool)
	for _, fn := range c.order {
		set := make(map[int]bool)
		sig := fn.Signature()
		f := &flow{c: c, onCall: func(call *ast.CallExpr) {
			if i := c.paramIndex(sig, call.Fun); i >= 0 {
				set[i] = true
			}
		}}
		f.walk(c.decls[fn].Body)
		found[fn] = set
	}
	return found
}

func (c *checker) regions(list []ast.Stmt, visit func(lockKey, []ast.Stmt, []ast.Stmt)) {
	for i, s := range list {
		key := c.lockStmtKey(s)
		if key.root == nil {
			continue
		}
		end := i + 1
		for end < len(list) && !c.unlocks(list[end], key) {
			end++
		}
		visit(key, list[i+1:end], list[end:])
	}
}

func (c *checker) unlocks(s ast.Stmt, key lockKey) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.DeferStmt, *ast.GoStmt, *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = found || c.mutexKey(n, "Unlock") == key
		}
		return !found
	})
	return found
}

func (c *checker) deferredUnlockAfter(region []ast.Stmt, key lockKey, d *ast.DeferStmt) bool {
	for _, s := range region {
		if u, ok := s.(*ast.DeferStmt); ok && d.Pos() < u.Pos() && c.mutexKey(u.Call, "Unlock") == key {
			return true
		}
	}
	return false
}

func (c *checker) findHeldCalls() map[*types.Func][]heldCall {
	found := make(map[*types.Func][]heldCall)
	for _, fn := range c.order {
		f := &flow{c: c}
		f.onStmts = func(list []ast.Stmt) {
			c.regions(list, func(key lockKey, region, tail []ast.Stmt) {
				found[fn] = append(found[fn], c.regionHeldCalls(fn, key, region, tail)...)
			})
		}
		f.walk(c.decls[fn].Body)
	}
	return found
}

func (c *checker) regionHeldCalls(fn *types.Func, key lockKey, region, tail []ast.Stmt) []heldCall {
	var found []heldCall
	sig := fn.Signature()
	f := &flow{c: c, deferHeld: c.deferHeldIn(key, region, tail), onCall: func(call *ast.CallExpr) {
		if i := c.paramIndex(sig, call.Fun); i >= 0 {
			found = append(found, heldCall{key: key, param: i})
		}
	}}
	f.walkStmts(region)
	return found
}

func (c *checker) deferHeldIn(key lockKey, region, tail []ast.Stmt) func(*ast.DeferStmt) bool {
	return func(d *ast.DeferStmt) bool {
		return len(tail) == 0 && !c.deferredUnlockAfter(region, key, d)
	}
}

func (c *checker) summarize() {
	calls := make(map[*types.Func][]*ast.CallExpr)
	for _, fn := range c.order {
		f := &flow{c: c, onCall: func(call *ast.CallExpr) {
			calls[fn] = append(calls[fn], call)
		}}
		f.walk(c.decls[fn].Body)
		c.summaries[fn] = make(map[lockKey]bool)
	}
	c.limit = c.pathLimit(calls)
	for grew := true; grew; {
		grew = c.grow(calls)
	}
}

func (c *checker) pathLimit(calls map[*types.Func][]*ast.CallExpr) int {
	limit := -1
	for _, list := range calls {
		for _, call := range list {
			limit = max(limit, c.heldPath(call))
		}
	}
	return limit
}

func (c *checker) heldPath(call *ast.CallExpr) int {
	limit := -1
	if k := c.mutexKey(call, "Lock"); k.root != nil {
		limit = len(k.path)
	}
	fn := c.callee(call)
	for _, h := range c.heldCalls[fn] {
		if k := c.substitute(h.key, call, fn); k.root != nil {
			limit = max(limit, len(k.path))
		}
	}
	return limit
}

func (c *checker) grow(calls map[*types.Func][]*ast.CallExpr) bool {
	grew := false
	for fn, list := range calls {
		for _, call := range list {
			grew = c.add(c.summaries[fn], c.reach(call)) || grew
		}
	}
	return grew
}

func (c *checker) add(summary map[lockKey]bool, keys []lockKey) bool {
	grew := false
	for _, k := range keys {
		if k.root != nil && len(k.path) <= c.limit && !summary[k] {
			summary[k] = true
			grew = true
		}
	}
	return grew
}

func (c *checker) reach(call *ast.CallExpr) []lockKey {
	if k := c.mutexKey(call, "Lock"); k.root != nil {
		return []lockKey{k}
	}
	fn := c.callee(call)
	if fn == nil {
		return nil
	}
	return append(c.callKeys(fn, call), c.callbackKeys(fn, call)...)
}

func (c *checker) callKeys(fn *types.Func, call *ast.CallExpr) []lockKey {
	var keys []lockKey
	for k := range c.summaries[fn] {
		if s := c.substitute(k, call, fn); s.root != nil {
			keys = append(keys, s)
		}
	}
	return keys
}

func (c *checker) callbackKeys(fn *types.Func, call *ast.CallExpr) []lockKey {
	var keys []lockKey
	for i := range c.invokes[fn] {
		if i >= len(call.Args) {
			continue
		}
		target, b := c.namedCallback(call.Args[i])
		for k := range c.summaries[target] {
			if bk := b.bindKey(k); bk.root != nil {
				keys = append(keys, bk)
			}
		}
	}
	return keys
}

func (c *checker) namedCallback(arg ast.Expr) (*types.Func, binding) {
	var id *ast.Ident
	var sel *ast.SelectorExpr
	switch x := ast.Unparen(arg).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id, sel = x.Sel, x
	default:
		return nil, binding{}
	}
	fn, ok := c.pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil, binding{}
	}
	fn = fn.Origin()
	decl, ok := c.decls[fn]
	if !ok {
		return nil, binding{}
	}
	return fn, binding{recv: c.receiverBinding(fn, sel), decl: decl}
}

func (c *checker) receiverBinding(fn *types.Func, sel *ast.SelectorExpr) map[types.Object]lockKey {
	r := fn.Signature().Recv()
	if sel == nil || r == nil {
		return nil
	}
	k := c.recvKey(sel, sel.X)
	if k.root == nil {
		return nil
	}
	return map[types.Object]lockKey{r: k}
}

func (c *checker) check(decl *ast.FuncDecl) {
	f := &flow{c: c, onCall: c.checkHeldCallbacks}
	f.onStmts = func(list []ast.Stmt) {
		c.regions(list, func(key lockKey, region, tail []ast.Stmt) {
			held := c.heldFlow(key, binding{})
			held.deferHeld = c.deferHeldIn(key, region, tail)
			held.walkStmts(region)
		})
	}
	f.walk(decl.Body)
}

func (c *checker) heldFlow(key lockKey, b binding) *flow {
	return &flow{c: c, onCall: func(call *ast.CallExpr) {
		c.checkInner(call, key, b)
	}}
}

func (c *checker) checkHeldCallbacks(call *ast.CallExpr) {
	fn := c.callee(call)
	for _, h := range c.heldCalls[fn] {
		key := c.substitute(h.key, call, fn)
		if key.root != nil && h.param < len(call.Args) {
			c.checkCallback(key, call.Args[h.param])
		}
	}
}

func (c *checker) checkCallback(key lockKey, arg ast.Expr) {
	if lit, ok := ast.Unparen(arg).(*ast.FuncLit); ok {
		c.heldFlow(key, binding{}).walk(lit.Body)
		return
	}
	if target, b := c.namedCallback(arg); target != nil {
		c.heldFlow(key, b).walk(b.decl.Body)
	}
}

func (c *checker) checkInner(call *ast.CallExpr, key lockKey, b binding) {
	if k := c.mutexKey(call, "Lock"); k.root != nil {
		if b.bindKey(k) == key {
			c.report(call, fmt.Sprintf(relockFormat, key))
		}
		return
	}
	fn := c.callee(call)
	if fn == nil {
		return
	}
	for _, k := range append(c.callKeys(fn, call), c.callbackKeys(fn, call)...) {
		if b.bindKey(k) == key {
			c.report(call, fmt.Sprintf(callFormat, fn.Name(), key))
			return
		}
	}
}

func (c *checker) report(call *ast.CallExpr, msg string) {
	if c.reported[call] {
		return
	}
	c.reported[call] = true
	c.pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: msg})
}
