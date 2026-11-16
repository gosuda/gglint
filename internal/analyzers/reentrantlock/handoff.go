package reentrantlock

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/types/typeutil"
)

// summarizeReleases records possible unlocks by release-only functions. Reject
// acquisitions and mutations in their synchronous call graph: aliases can hide
// a lock acquisition or rebind a receiver. summarize seeds mutation and spawn
// barriers before these call effects propagate.
// The existing field-path limit bounds recursive substitutions.
func (c *checker) summarizeReleases(calls map[*types.Func][]*ast.CallExpr) {
	for grew := true; grew; {
		grew = c.growBarriers(calls)
	}
	c.releases = make(map[*types.Func]map[lockKey]bool)
	for grew := true; grew; {
		grew = c.growReleases(calls)
	}
}

func (c *checker) growBarriers(calls map[*types.Func][]*ast.CallExpr) bool {
	grew := false
	for _, fn := range c.order {
		if !c.releaseBarrier[fn] && slices.ContainsFunc(calls[fn], c.callBlocksRelease) {
			c.releaseBarrier[fn] = true
			grew = true
		}
	}
	return grew
}

func (c *checker) growReleases(calls map[*types.Func][]*ast.CallExpr) bool {
	grew := false
	for _, fn := range c.order {
		if c.releaseBarrier[fn] {
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
	return grew
}

func (c *checker) callBlocksRelease(call *ast.CallExpr) bool {
	method := typeutil.StaticCallee(c.pass.TypesInfo, call)
	// Invoked literals are inspected by flow.call. Other unresolved calls may
	// acquire a mutex or change the pointer that names it.
	if method == nil {
		return !c.safeCallOperand(call)
	}
	if isMutexMethod(method, "Unlock") || isMutexMethod(method, "RUnlock") {
		return false
	}
	if scalarAtomic(method) {
		return false
	}
	fn := c.callee(call)
	return fn == nil || c.releaseBarrier[fn]
}

func (c *checker) safeCallOperand(call *ast.CallExpr) bool {
	if _, literal := ast.Unparen(call.Fun).(*ast.FuncLit); literal {
		return true
	}
	if typ := c.pass.TypesInfo.Types[call.Fun]; typ.IsType() {
		return safeConversionType(typ.Type) && safeConversionType(c.pass.TypesInfo.TypeOf(call.Args[0]))
	}
	builtin, ok := typeutil.Callee(c.pass.TypesInfo, call).(*types.Builtin)
	if !ok || builtin.Pkg() != nil {
		return false
	}
	switch builtin.Name() {
	case "append", "clear", "copy", "delete", "panic", "recover":
		return false
	default:
		return true
	}
}

func safeConversionType(t types.Type) bool {
	if _, parameter := types.Unalias(t).(*types.TypeParam); parameter {
		return false
	}
	basic, ok := t.Underlying().(*types.Basic)
	return !ok || basic.Kind() != types.UnsafePointer
}

// Do not infer a release from a statement after an unconditional return. This
// recognizes straight-line blocks and if/else arms; other branch constructs
// already prevent release inference rather than requiring a control-flow model.
func hasEarlyReturn(list []ast.Stmt) bool {
	for i, s := range list {
		if i+1 < len(list) && alwaysReturns(s) {
			return true
		}
	}
	return false
}

func alwaysReturns(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStmt:
		return len(s.List) != 0 && alwaysReturns(s.List[len(s.List)-1])
	case *ast.IfStmt:
		return s.Else != nil && alwaysReturns(s.Body) && alwaysReturns(s.Else)
	case *ast.LabeledStmt:
		return alwaysReturns(s.Stmt)
	default:
		return false
	}
}

// Scalar atomics cannot retarget a pointer carrier. Pointer and Value atomics
// stay opaque, and unsafe operations that could alias scalar storage are barred.
// Check the actual package path, not an import alias or package name.
func scalarAtomic(fn *types.Func) bool {
	if pkg := fn.Pkg(); pkg == nil || pkg.Path() != "sync/atomic" {
		return false
	}
	sig := fn.Signature()
	if recv := sig.Recv(); recv != nil {
		named, ok := deref(recv.Type()).(*types.Named)
		if !ok {
			return false
		}
		switch named.Obj().Name() {
		case "Bool", "Int32", "Int64", "Uint32", "Uint64", "Uintptr":
			return true
		default:
			return false
		}
	}
	for param := range sig.Params().Variables() {
		basic, ok := deref(param.Type()).Underlying().(*types.Basic)
		if !ok || basic.Info()&types.IsInteger == 0 {
			return false
		}
	}
	return sig.Params().Len() != 0
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
		if recv := fn.Signature().Recv(); recv != nil && k.root == recv {
			if _, pointer := recv.Type().Underlying().(*types.Pointer); !pointer {
				continue
			}
		}
		if s := c.substitute(k, call, fn); s.root != nil {
			keys = append(keys, s)
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
	releases, blocked := false, false
	f := &flow{c: c, onCall: func(call *ast.CallExpr) {
		for _, k := range c.releaseKeys(call) {
			if _, bound := b.recv[k.root]; bound {
				if _, pointer := k.root.Type().Underlying().(*types.Pointer); !pointer {
					continue
				}
			}
			releases = releases || b.bindKey(k) == key
		}
		blocked = blocked || c.callBlocksRelease(call)
	}, onGo: func(*ast.GoStmt) {
		blocked = true
	}, onBarrier: func() {
		blocked = true
	}, onStmts: func(list []ast.Stmt) {
		blocked = blocked || hasEarlyReturn(list)
	}}
	f.call(call)
	return releases && !blocked
}
