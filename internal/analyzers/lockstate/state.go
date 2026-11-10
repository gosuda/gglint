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
	if def, ok := declValue(info, fn, v); ok && !emptyLiteral(def) {
		return unknownState
	}
	if lockExposed(info, fn, key) {
		return unknownState
	}
	return freeState
}

func declValue(info *types.Info, fn ast.Node, v *types.Var) (ast.Expr, bool) {
	body := flow.Body(fn)
	if info == nil || body == nil || v == nil {
		return nil, false
	}
	if e, ok := defineIn(info, fn, body, v); ok {
		return e, true
	}
	return varIn(info, fn, body, v)
}

func defineIn(info *types.Info, fn ast.Node, body *ast.BlockStmt, v *types.Var) (ast.Expr, bool) {
	var out ast.Expr
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		e, ok := defineValue(info, as, v)
		if !ok {
			return true
		}
		out = e
		found = true
		return false
	})
	return out, found
}

func varIn(info *types.Info, fn ast.Node, body *ast.BlockStmt, v *types.Var) (ast.Expr, bool) {
	var out ast.Expr
	matched := false
	seen := false
	ast.Inspect(body, func(n ast.Node) bool {
		if seen {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if !declaresVar(info, vs, v) {
			return true
		}
		seen = true
		e, ok := varValueFor(vs, info, v)
		if !ok {
			return false
		}
		out = e
		matched = true
		return false
	})
	return out, matched
}

func defineValue(info *types.Info, as *ast.AssignStmt, v *types.Var) (ast.Expr, bool) {
	if as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return nil, false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || id.Name == "_" || info.Defs[id] != v {
		return nil, false
	}
	return as.Rhs[0], true
}

func declaresVar(info *types.Info, vs *ast.ValueSpec, v *types.Var) bool {
	for _, name := range vs.Names {
		if name.Name != "_" && info.Defs[name] == v {
			return true
		}
	}
	return false
}

func varValueFor(vs *ast.ValueSpec, info *types.Info, v *types.Var) (ast.Expr, bool) {
	if len(vs.Values) == 0 {
		return nil, false
	}
	for i, name := range vs.Names {
		if name.Name == "_" {
			continue
		}
		if info.Defs[name] != v {
			continue
		}
		if len(vs.Values) == 1 {
			return vs.Values[0], true
		}
		if i < len(vs.Values) {
			return vs.Values[i], true
		}
		return nil, false
	}
	return nil, false
}

func reassignsRoot(info *types.Info, n ast.Node, key flow.Key) bool {
	v, ok := key.Root.(*types.Var)
	if !ok {
		return false
	}
	hit := false
	ast.Inspect(n, func(x ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if nodeReassignsRoot(info, x, v) {
			hit = true
			return false
		}
		return true
	})
	return hit
}

func nodeReassignsRoot(info *types.Info, x ast.Node, v *types.Var) bool {
	if as, ok := x.(*ast.AssignStmt); ok {
		return assignTargetsVar(info, as, v)
	}
	return false
}

func addrInExpr(info *types.Info, e ast.Expr, key flow.Key) bool {
	handed := false
	ast.Inspect(e, func(x ast.Node) bool {
		if handed {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND && encloses(info, u.X, key) {
			handed = true
			return false
		}
		return true
	})
	return handed
}

func handsAddress(info *types.Info, n ast.Node, key flow.Key) bool {
	switch n := n.(type) {
	case *ast.DeferStmt:
		for _, arg := range n.Call.Args {
			if addrInExpr(info, arg, key) {
				return true
			}
		}
	case *ast.SendStmt:
		return addrInExpr(info, n.Value, key)
	case *ast.ReturnStmt:
		for _, r := range n.Results {
			if addrInExpr(info, r, key) {
				return true
			}
		}
	}
	return false
}

func assignTargetsVar(info *types.Info, as *ast.AssignStmt, v *types.Var) bool {
	if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
		return false
	}
	for _, lhs := range as.Lhs {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if ok && id.Name != "_" && info.Uses[id] == v {
			return true
		}
	}
	return false
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

func storedAddress(info *types.Info, fn ast.Node, key flow.Key) bool {
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
		switch n := n.(type) {
		case *ast.AssignStmt:
			if rhsStoresAddress(info, n.Rhs, key) {
				found = true
				return false
			}
		case *ast.ValueSpec:
			if rhsStoresAddress(info, n.Values, key) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func rhsStoresAddress(info *types.Info, rhs []ast.Expr, key flow.Key) bool {
	for _, e := range rhs {
		if storesAddrInExpr(info, e, key) {
			return true
		}
	}
	return false
}

func storesAddrInExpr(info *types.Info, e ast.Expr, key flow.Key) bool {
	stored := false
	ast.Inspect(e, func(n ast.Node) bool {
		if stored {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		u, ok := n.(*ast.UnaryExpr)
		if !ok {
			return true
		}
		if u.Op == token.AND && encloses(info, u.X, key) {
			stored = true
			return false
		}
		return true
	})
	return stored
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
		return inexactReadState(min(a.depth, b.depth))
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
	if reassignsRoot(info, n, key) {
		return unknownState
	}
	if handsAddress(info, n, key) {
		return unknownState
	}
	if ds, ok := n.(*ast.DeferStmt); ok {
		return deferArgs(info, key, ds, cur)
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
	for _, arg := range ds.Call.Args {
		if addrInExpr(info, arg, key) {
			return unknownState
		}
	}
	next := cur
	for _, arg := range ds.Call.Args {
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

func collectPre(g *flow.Graph, entry state, tr func(ast.Node, state) state) (map[ast.Node]state, []ast.Node) {
	out := map[ast.Node]state{}
	var order []ast.Node
	flow.Forward(g, entry, tr, joinState, func(n ast.Node, s state) {
		if _, ok := out[n]; !ok {
			order = append(order, n)
		}
		out[n] = s
	})
	return out, order
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
