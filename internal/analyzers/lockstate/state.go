package lockstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

type state uint8

const (
	bottomState state = iota
	freeState
	writeState
	readState
	unknownState
)

const (
	methodLock     = "Lock"
	methodRLock    = "RLock"
	methodUnlock   = "Unlock"
	methodRUnlock  = "RUnlock"
	methodTryLock  = "TryLock"
	methodTryRLock = "TryRLock"
)

func methodName(info *types.Info, call *ast.CallExpr) string {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return ""
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock":
		return methodLock
	case "(*sync.RWMutex).RLock":
		return methodRLock
	case "(*sync.Mutex).Unlock", "(*sync.RWMutex).Unlock":
		return methodUnlock
	case "(*sync.RWMutex).RUnlock":
		return methodRUnlock
	case "(*sync.Mutex).TryLock", "(*sync.RWMutex).TryLock":
		return methodTryLock
	case "(*sync.RWMutex).TryRLock":
		return methodTryRLock
	}
	return ""
}

func mutexKey(info *types.Info, call *ast.CallExpr) (flow.Key, string, bool) {
	m := methodName(info, call)
	if m == "" {
		return flow.Key{}, "", false
	}
	k, ok := flow.MethodKey(info, call)
	if !ok {
		return flow.Key{}, "", false
	}
	return k, m, true
}

func recvExpr(info *types.Info, call *ast.CallExpr) ast.Expr {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || info == nil {
		return nil
	}
	s := info.Selections[sel]
	if s == nil {
		return nil
	}
	if s.Kind() == types.MethodExpr {
		if len(call.Args) == 0 {
			return nil
		}
		return call.Args[0]
	}
	if s.Kind() != types.MethodVal {
		return nil
	}
	return sel.X
}

func recvText(info *types.Info, call *ast.CallExpr) string {
	e := recvExpr(info, call)
	if e == nil {
		return ""
	}
	return types.ExprString(e)
}

func entryState(info *types.Info, fn ast.Node, key flow.Key) state {
	v, ok := key.Root.(*types.Var)
	if !ok {
		return unknownState
	}
	if flow.IsLocal(info, fn, v) && !flow.Escapes(info, fn, v) {
		return freeState
	}
	return unknownState
}

func takesAddress(info *types.Info, n ast.Node, key flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		u, ok := x.(*ast.UnaryExpr)
		if !ok {
			return true
		}
		if encloses(info, u.X, key) {
			found = true
			return false
		}
		return true
	})
	return found
}

func collectKeys(info *types.Info, fn ast.Node) []flow.Key {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	seen := map[flow.Key]bool{}
	var out []flow.Key
	ast.Inspect(fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, _, ok := mutexKey(info, call)
		if !ok || seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return out
}

func joinState(a, b state) state {
	if a == bottomState {
		return b
	}
	if b == bottomState {
		return a
	}
	if a == b {
		return a
	}
	return unknownState
}

func stepMethod(m string, cur state) state {
	switch m {
	case methodLock:
		return writeState
	case methodRLock:
		if cur == writeState {
			return unknownState
		}
		return readState
	case methodUnlock:
		if cur == writeState {
			return freeState
		}
		return unknownState
	case methodRUnlock:
		if cur == readState {
			return freeState
		}
		return unknownState
	}
	return unknownState
}

func takesKey(info *types.Info, call *ast.CallExpr, key flow.Key) bool {
	for _, arg := range call.Args {
		if encloses(info, arg, key) {
			return true
		}
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if encloses(info, sel.X, key) {
			return true
		}
	}
	return false
}

func encloses(info *types.Info, e ast.Expr, key flow.Key) bool {
	k, ok := flow.KeyOf(info, e)
	if !ok || k.Root != key.Root {
		return false
	}
	if k == key {
		return true
	}
	if k.Path == "" || key.Path == "" {
		return true
	}
	return prefixPath(key.Path, k.Path) || prefixPath(k.Path, key.Path)
}

func prefixPath(p, q string) bool {
	if len(p) <= len(q) || p[:len(q)] != q {
		return false
	}
	return p[len(q)] == '.'
}

func mentionsKey(info *types.Info, lit *ast.FuncLit, key flow.Key) bool {
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if found {
			return false
		}
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		k, ok := flow.KeyOf(info, e)
		if !ok {
			return true
		}
		if k.Root != key.Root {
			return false
		}
		if encloses(info, e, key) {
			found = true
		}
		return false
	})
	return found
}

func applyNode(info *types.Info, key flow.Key, n ast.Node, cur state) state {
	if cur == bottomState {
		return bottomState
	}
	if _, ok := n.(*ast.DeferStmt); ok {
		return cur
	}
	if takesAddress(info, n, key) {
		return unknownState
	}
	next := cur
	ast.Inspect(n, func(x ast.Node) bool {
		var cont bool
		next, cont = visitNode(info, key, x, next)
		return cont
	})
	return next
}

func visitNode(info *types.Info, key flow.Key, x ast.Node, next state) (state, bool) {
	if lit, ok := x.(*ast.FuncLit); ok {
		if mentionsKey(info, lit, key) {
			return unknownState, false
		}
		return next, false
	}
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return next, true
	}
	if k, m, matched := mutexKey(info, call); matched && k == key {
		return stepMethod(m, next), true
	}
	if takesKey(info, call, key) {
		return unknownState, true
	}
	return next, true
}

func collectPre(g *flow.Graph, entry state, tr func(ast.Node, state) state) map[ast.Node]state {
	out := map[ast.Node]state{}
	flow.Forward(g, entry, tr, joinState, func(n ast.Node, s state) {
		out[n] = s
	})
	return out
}

type opCall struct {
	call   *ast.CallExpr
	method string
	recv   string
}

func opsInNode(info *types.Info, n ast.Node, key flow.Key) []opCall {
	var out []opCall
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, m, ok := mutexKey(info, call)
		if !ok || k != key {
			return true
		}
		out = append(out, opCall{call: call, method: m, recv: recvText(info, call)})
		return true
	})
	return out
}
