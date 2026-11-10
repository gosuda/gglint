package flow

import (
	"cmp"
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// HeldAt returns, for a node, the mutex keys definitely held before it.
func HeldAt(pass *analysis.Pass, g *Graph) func(n ast.Node) []Key {
	none := func(ast.Node) []Key { return nil }
	if pass == nil || pass.TypesInfo == nil || g == nil {
		return none
	}
	info := pass.TypesInfo
	keys := mutexKeys(info, g.fn)
	held := foldKeys(info, g, keys)
	sortHeld(held)
	return func(n ast.Node) []Key {
		r := g.locate(n)
		if r == nil {
			return nil
		}
		return slices.Clone(held[r])
	}
}

func foldKeys(info *types.Info, g *Graph, keys []Key) map[ast.Node][]Key {
	held := map[ast.Node][]Key{}
	for _, k := range keys {
		Forward(g, false,
			func(n ast.Node, h bool) bool { return lockTransfer(info, n, k, h) },
			func(a, b bool) bool { return a && b },
			func(n ast.Node, h bool) {
				if h {
					held[n] = append(held[n], k)
				}
			})
	}
	return held
}

func sortHeld(held map[ast.Node][]Key) {
	for _, s := range held {
		slices.SortFunc(s, func(a, b Key) int {
			if c := cmp.Compare(a.Root.Pos(), b.Root.Pos()); c != 0 {
				return c
			}
			return cmp.Compare(a.Path, b.Path)
		})
	}
}

func mutexKeys(info *types.Info, fn ast.Node) []Key {
	var keys []Key
	seen := map[Key]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && n != fn && !calledInPlace(fn, lit) {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || mutexMethod(typeutil.StaticCallee(info, call)) == "" {
			return true
		}
		if k, ok := MethodKey(info, call); ok && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		return true
	})
	return keys
}

func calledInPlace(scope ast.Node, lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(scope, func(n ast.Node) bool {
		if found {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && ast.Unparen(c.Fun) == lit {
			found = true
			return false
		}
		return true
	})
	return found
}

func lockTransfer(info *types.Info, n ast.Node, k Key, h bool) bool {
	out := h
	ast.Inspect(n, func(x ast.Node) bool {
		return transferStep(info, n, k, x, &out)
	})
	return out
}

func transferStep(info *types.Info, scope ast.Node, k Key, x ast.Node, out *bool) bool {
	if lit, ok := x.(*ast.FuncLit); ok && calledInPlace(scope, lit) {
		*out = inPlaceEffect(info, lit, k, *out)
		return false
	}
	if _, ok := x.(*ast.FuncLit); ok {
		return false
	}
	switch x.(type) {
	case *ast.GoStmt, *ast.DeferStmt:
		return false
	}
	return transferCall(info, k, x, out)
}

func transferCall(info *types.Info, k Key, x ast.Node, out *bool) bool {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return true
	}
	m := mutexMethod(typeutil.StaticCallee(info, call))
	if m == "" {
		return true
	}
	ck, ok := MethodKey(info, call)
	if !ok || ck != k {
		return true
	}
	if m == opLock || m == opRLock {
		*out = true
	}
	if m == opUnlock || m == opRUnlock {
		*out = false
	}
	return true
}

func mutexMethod(fn *types.Func) string {
	if fn == nil {
		return ""
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock":
		return opLock
	case "(*sync.RWMutex).RLock":
		return opRLock
	case "(*sync.Mutex).Unlock", "(*sync.RWMutex).Unlock":
		return opUnlock
	case "(*sync.RWMutex).RUnlock":
		return opRUnlock
	case "(*sync.Mutex).TryLock", "(*sync.RWMutex).TryLock":
		return opTryLock
	case "(*sync.RWMutex).TryRLock":
		return opTryRLock
	}
	return ""
}

const (
	opLock     = "Lock"
	opRLock    = "RLock"
	opUnlock   = "Unlock"
	opRUnlock  = "RUnlock"
	opTryLock  = "TryLock"
	opTryRLock = "TryRLock"
)

func inPlaceEffect(info *types.Info, lit *ast.FuncLit, k Key, incoming bool) bool {
	if lit.Body == nil {
		return incoming
	}
	if directLockHeld(info, lit, k) && !anyUnlock(info, lit, k) {
		return true
	}
	if anyUnlock(info, lit, k) {
		return false
	}
	return incoming
}

func directLockHeld(info *types.Info, lit *ast.FuncLit, k Key) bool {
	for _, s := range lit.Body.List {
		if stmtLocks(info, s, k) {
			return true
		}
	}
	return false
}

func stmtLocks(info *types.Info, s ast.Node, k Key) bool {
	locked := false
	ast.Inspect(s, func(x ast.Node) bool {
		if locked {
			return false
		}
		switch x.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		if call, ok := x.(*ast.CallExpr); ok {
			if isLockOp(info, call, k) {
				locked = true
				return false
			}
		}
		return true
	})
	return locked
}

func anyUnlock(info *types.Info, lit *ast.FuncLit, k Key) bool {
	found := false
	called := calledLiterals(lit.Body)
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.GoStmt); ok {
			return false
		}
		if f, ok := x.(*ast.FuncLit); ok {
			if _, ok := called[f]; !ok {
				return false
			}
			return true
		}
		if call, ok := x.(*ast.CallExpr); ok && isUnlockOp(info, call, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isLockOp(info *types.Info, call *ast.CallExpr, k Key) bool {
	m := mutexMethod(typeutil.StaticCallee(info, call))
	if m != opLock && m != opRLock {
		return false
	}
	ck, ok := MethodKey(info, call)
	return ok && ck == k
}

func isUnlockOp(info *types.Info, call *ast.CallExpr, k Key) bool {
	m := mutexMethod(typeutil.StaticCallee(info, call))
	if m != opUnlock && m != opRUnlock {
		return false
	}
	ck, ok := MethodKey(info, call)
	return ok && ck == k
}
