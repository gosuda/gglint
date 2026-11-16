package reentrantlock

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// summarizeReleases records possible unlocks by release-only functions. Reject
// any recognized acquisition in their synchronous call graph: a local alias or
// call-result receiver could name the held mutex without producing its key.
// The existing field-path limit bounds recursive substitutions.
func (c *checker) summarizeReleases(calls map[*types.Func][]*ast.CallExpr) {
	c.releases = make(map[*types.Func]map[lockKey]bool)
	c.acquires = make(map[*types.Func]bool)
	for grew := true; grew; {
		grew = false
		for _, fn := range c.order {
			if c.acquires[fn] {
				continue
			}
			for _, call := range calls[fn] {
				if c.callAcquires(call) {
					c.acquires[fn] = true
					grew = true
					break
				}
			}
		}
	}
	for grew := true; grew; {
		grew = false
		for _, fn := range c.order {
			if c.acquires[fn] {
				continue
			}
			for _, call := range calls[fn] {
				keys := c.releaseKeys(call)
				if len(keys) == 0 {
					continue
				}
				if c.releases[fn] == nil {
					c.releases[fn] = make(map[lockKey]bool)
				}
				grew = c.add(c.releases[fn], keys) || grew
			}
		}
	}
}

func (c *checker) callAcquires(call *ast.CallExpr) bool {
	method := typeutil.StaticCallee(c.pass.TypesInfo, call)
	if isMutexMethod(method, "Lock") || isMutexMethod(method, "RLock") {
		return true
	}
	// A function value or interface dispatch can hide an acquisition. Invoked
	// literals are inspected by flow.call; conversions and builtins cannot lock.
	if method == nil && !c.pass.TypesInfo.Types[call.Fun].IsType() {
		_, builtin := typeutil.Callee(c.pass.TypesInfo, call).(*types.Builtin)
		_, literal := ast.Unparen(call.Fun).(*ast.FuncLit)
		if !builtin && !literal {
			return true
		}
	}
	fn := c.callee(call)
	if c.acquires[fn] {
		return true
	}
	_, _, args := c.callOperands(call)
	for i := range c.invokes[fn] {
		if i < len(args) {
			target, _ := c.namedCallback(args[i])
			if c.acquires[target] {
				return true
			}
		}
	}
	return false
}

func (c *checker) releaseKeys(call *ast.CallExpr) []lockKey {
	if k := c.mutexKey(call, "Unlock"); k.root != nil {
		return []lockKey{k}
	}
	fn := c.callee(call)
	if fn == nil {
		return nil
	}
	var keys []lockKey
	for k := range c.releases[fn] {
		if s := c.substitute(k, call, fn); s.root != nil {
			keys = append(keys, s)
		}
	}
	_, _, args := c.callOperands(call)
	for i := range c.invokes[fn] {
		if i >= len(args) {
			continue
		}
		target, b := c.namedCallback(args[i])
		for k := range c.releases[target] {
			if s := b.bindKey(k); s.root != nil {
				keys = append(keys, s)
			}
		}
	}
	return keys
}

// handsOff describes a possible release, not proof that the whole program is
// free from deadlock. Inspect only invoked code; a stored function literal does
// not release a lock. The caller evaluates go operands and arguments before
// applying this effect, so a deadlock in those expressions still reports.
func (c *checker) handsOff(call *ast.CallExpr, key lockKey, b binding) bool {
	// The first held-callback pass supplies the acquisition path bound. Keep
	// it conservative until the complete acquisition summaries are available.
	if c.releases == nil {
		return false
	}
	releases, acquires := false, false
	f := &flow{c: c, onCall: func(call *ast.CallExpr) {
		for _, k := range c.releaseKeys(call) {
			releases = releases || b.bindKey(k) == key
		}
		acquires = acquires || c.callAcquires(call)
	}}
	f.call(call)
	return releases && !acquires
}
