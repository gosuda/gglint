package lockstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

type stateKind uint8

const (
	bottomKind stateKind = iota
	freeKind
	writeKind
	readKind
	unknownKind
)

type state struct {
	kind  stateKind
	depth int
	exact bool
}

var (
	bottomState  = state{kind: bottomKind}
	freeState    = state{kind: freeKind}
	writeState   = state{kind: writeKind}
	unknownState = state{kind: unknownKind}
)

func readState(depth int) state {
	return state{kind: readKind, depth: depth, exact: true}
}

func inexactReadState(depth int) state {
	return state{kind: readKind, depth: depth}
}

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
	if !flow.IsLocal(info, fn, v) {
		return unknownState
	}
	if _, ok := v.Type().Underlying().(*types.Pointer); ok {
		return unknownState
	}
	if pathHasPointer(v.Type(), key.Path) {
		return unknownState
	}
	if def, ok := flow.DefinedBy(info, fn, v); ok && !emptyLiteral(def) {
		return unknownState
	}
	if lockExposed(info, fn, key) {
		return unknownState
	}
	return freeState
}

func emptyLiteral(e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	return ok && len(lit.Elts) == 0
}

func lockExposed(info *types.Info, fn ast.Node, key flow.Key) bool {
	funs := mutexFunSet(info, fn, key)
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if exposesAt(info, fn, n, key, funs) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exposesAt(info *types.Info, fn, n ast.Node, key flow.Key, funs map[ast.Expr]bool) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.AND && encloses(info, n.X, key)
	case *ast.SelectorExpr:
		return exposesMethodValue(info, n, key, funs)
	case *ast.CallExpr:
		return exposesCall(info, n, key)
	}
	return exposesStmtAt(info, fn, n, key)
}

func exposesStmtAt(info *types.Info, fn, n ast.Node, key flow.Key) bool {
	switch n := n.(type) {
	case *ast.ReturnStmt:
		return exposesResults(info, n.Results, key)
	case *ast.SendStmt:
		return encloses(info, n.Value, key)
	case *ast.GoStmt:
		return takesKey(info, n.Call, key)
	case *ast.FuncLit:
		return n != fn && mentionsKey(info, n, key)
	case *ast.AssignStmt:
		return storedEscape(info, fn, n, key)
	}
	return false
}

func exposesCall(info *types.Info, call *ast.CallExpr, key flow.Key) bool {
	if k, _, ok := mutexKey(info, call); ok && k == key {
		return false
	}
	return takesKey(info, call, key)
}

func exposesResults(info *types.Info, results []ast.Expr, key flow.Key) bool {
	for _, r := range results {
		if encloses(info, r, key) {
			return true
		}
	}
	return false
}

func exposesMethodValue(info *types.Info, sel *ast.SelectorExpr, key flow.Key, funs map[ast.Expr]bool) bool {
	if funs[sel] {
		return false
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal || !isLockMethodName(s.Obj().Name()) {
		return false
	}
	return encloses(info, sel.X, key)
}

func isLockMethodName(m string) bool {
	switch m {
	case methodLock, methodRLock, methodUnlock, methodRUnlock, methodTryLock, methodTryRLock:
		return true
	}
	return false
}

func storedEscape(info *types.Info, fn ast.Node, s *ast.AssignStmt, key flow.Key) bool {
	if !storedLHS(info, fn, s.Lhs) {
		return false
	}
	for _, rhs := range s.Rhs {
		if encloses(info, rhs, key) {
			return true
		}
	}
	return false
}

func storedLHS(info *types.Info, fn ast.Node, lhs []ast.Expr) bool {
	for _, e := range lhs {
		if storedTarget(info, fn, e) {
			return true
		}
	}
	return false
}

func storedTarget(info *types.Info, fn ast.Node, lhs ast.Expr) bool {
	switch l := ast.Unparen(lhs).(type) {
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.StarExpr:
		return true
	case *ast.Ident:
		v, ok := info.Defs[l].(*types.Var)
		return ok && (v.Pos() < fn.Pos() || v.Pos() >= fn.End())
	}
	return false
}

func mutexFunSet(info *types.Info, fn ast.Node, key flow.Key) map[ast.Expr]bool {
	funs := map[ast.Expr]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if k, _, ok := mutexKey(info, call); ok && k == key {
				funs[ast.Unparen(call.Fun)] = true
			}
		}
		return true
	})
	return funs
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
	if a.kind == bottomKind {
		return b
	}
	if b.kind == bottomKind {
		return a
	}
	if a == b {
		return a
	}
	if a.kind == readKind && b.kind == readKind && (!a.exact || !b.exact) {
		depth := a.depth
		if b.depth < depth {
			depth = b.depth
		}
		return inexactReadState(depth)
	}
	return unknownState
}

func stepMethod(m string, cur state) state {
	switch m {
	case methodLock:
		return writeState
	case methodRLock:
		return stepRLock(cur)
	case methodUnlock:
		return stepUnlock(cur)
	case methodRUnlock:
		return stepRUnlock(cur)
	}
	return unknownState
}

func stepRLock(cur state) state {
	if cur.kind == writeKind {
		return unknownState
	}
	if cur.kind == readKind {
		if !cur.exact {
			return inexactReadState(cur.depth + 1)
		}
		return readState(cur.depth + 1)
	}
	if cur.kind == unknownKind {
		return inexactReadState(1)
	}
	return readState(1)
}

func stepUnlock(cur state) state {
	if cur.kind == writeKind {
		return freeState
	}
	return unknownState
}

func stepRUnlock(cur state) state {
	if cur.kind == readKind && !cur.exact {
		if cur.depth >= 2 {
			return inexactReadState(cur.depth - 1)
		}
		return unknownState
	}
	if cur.kind == readKind && cur.depth > 1 {
		return readState(cur.depth - 1)
	}
	if cur.kind == readKind {
		return freeState
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
	if ds, ok := n.(*ast.DeferStmt); ok {
		return deferArgs(info, key, ds, cur)
	}
	if takesAddress(info, n, key) {
		return unknownState
	}
	skips := mutexFunSet(info, n, key)
	next := cur
	ast.Inspect(n, func(x ast.Node) bool {
		var cont bool
		next, cont = visitNode(info, key, x, next, skips)
		return cont
	})
	return next
}

func deferArgs(info *types.Info, key flow.Key, ds *ast.DeferStmt, cur state) state {
	next := cur
	for _, arg := range ds.Call.Args {
		if takesAddress(info, arg, key) {
			return unknownState
		}
		ast.Inspect(arg, func(x ast.Node) bool {
			var cont bool
			next, cont = visitNode(info, key, x, next, nil)
			return cont
		})
	}
	return next
}

func visitNode(info *types.Info, key flow.Key, x ast.Node, next state, skips map[ast.Expr]bool) (state, bool) {
	if gs, ok := x.(*ast.GoStmt); ok {
		return visitGoStmt(info, gs, key, next)
	}
	if lit, ok := x.(*ast.FuncLit); ok {
		return visitLit(info, lit, key, next)
	}
	if sel, ok := x.(*ast.SelectorExpr); ok {
		if exposesMethodValue(info, sel, key, skips) {
			return unknownState, false
		}
		return next, true
	}
	return visitCall(info, key, x, next)
}

func visitGoStmt(info *types.Info, gs *ast.GoStmt, key flow.Key, next state) (state, bool) {
	if goSharesKey(info, gs, key) {
		return unknownState, false
	}
	return next, false
}

func visitLit(info *types.Info, lit *ast.FuncLit, key flow.Key, next state) (state, bool) {
	if mentionsKey(info, lit, key) {
		return unknownState, false
	}
	return next, false
}

func visitCall(info *types.Info, key flow.Key, x ast.Node, next state) (state, bool) {
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

func goSharesKey(info *types.Info, gs *ast.GoStmt, key flow.Key) bool {
	shared := false
	ast.Inspect(gs.Call, func(n ast.Node) bool {
		if shared {
			return false
		}
		if goNodeSharesKey(info, n, key) {
			shared = true
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		return true
	})
	return shared
}

func goNodeSharesKey(info *types.Info, n ast.Node, key flow.Key) bool {
	if lit, ok := n.(*ast.FuncLit); ok {
		return mentionsKey(info, lit, key)
	}
	if call, ok := n.(*ast.CallExpr); ok {
		return takesKey(info, call, key)
	}
	if u, ok := n.(*ast.UnaryExpr); ok {
		return encloses(info, u.X, key)
	}
	return false
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
		if _, ok := x.(*ast.GoStmt); ok {
			return false
		}
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
