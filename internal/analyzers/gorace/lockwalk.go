package gorace

import (
	"cmp"
	"go/ast"
	"go/types"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

const (
	lockOpLock    = "Lock"
	lockOpUnlock  = "Unlock"
	lockOpRLock   = "RLock"
	lockOpRUnlock = "RUnlock"
)

type lockState int

const (
	lockFree lockState = iota
	lockHeld
)

type holder struct {
	keys     map[flow.Key]map[ast.Node]bool
	held     func(ast.Node) []flow.Key
	lazyHeld func() func(ast.Node) []flow.Key
}

func newHolder(pass *analysis.Pass, fn ast.Node) *holder {
	h := &holder{keys: map[flow.Key]map[ast.Node]bool{}}
	if pass == nil || pass.TypesInfo == nil || flow.Body(fn) == nil {
		return h
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return h
	}
	h.lazyHeld = func() func(ast.Node) []flow.Key { return flow.HeldAt(pass, g) }
	deferOps := deferStmts(fn)
	for _, k := range lockerKeys(pass.TypesInfo, fn) {
		seen := map[ast.Node]bool{}
		h.keys[k] = seen
		flow.Forward(g, lockFree,
			func(n ast.Node, s lockState) lockState { return stepLock(pass.TypesInfo, deferOps, k, n, s) },
			joinLockState,
			func(n ast.Node, s lockState) {
				if s == lockHeld {
					seen[n] = true
				}
			})
	}
	return h
}

func (h *holder) at(n ast.Node) []flow.Key {
	if h.held == nil && h.lazyHeld != nil {
		h.held = h.lazyHeld()
		h.lazyHeld = nil
	}
	resolved := resolveStmt(n)
	lookup := cmp.Or(resolved, n)
	seen := map[flow.Key]bool{}
	var out []flow.Key
	out = appendStaticKeys(out, seen, h.keys, n, resolved)
	out = appendFlowKeys(out, seen, h.held, lookup)
	return out
}

func appendStaticKeys(out []flow.Key, seen map[flow.Key]bool, keys map[flow.Key]map[ast.Node]bool, n, resolved ast.Node) []flow.Key {
	for k, nodes := range keys {
		if !nodes[n] && !nodes[resolved] {
			continue
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func appendFlowKeys(out []flow.Key, seen map[flow.Key]bool, held func(ast.Node) []flow.Key, lookup ast.Node) []flow.Key {
	if held == nil {
		return out
	}
	for _, k := range held(lookup) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func resolveStmt(n ast.Node) ast.Node {
	switch s := n.(type) {
	case *ast.IfStmt:
		return resolveIf(s)
	case *ast.ForStmt:
		return resolveFor(s)
	case *ast.RangeStmt:
		return resolveRange(s)
	case *ast.SwitchStmt:
		return resolveSwitch(s)
	case *ast.TypeSwitchStmt:
		return resolveTypeSwitch(s)
	case *ast.DeclStmt:
		return declSpec(s)
	default:
		return resolveClause(n)
	}
}

func resolveIf(s *ast.IfStmt) ast.Node {
	if s.Init != nil {
		return resolveStmt(s.Init)
	}
	return s.Cond
}

func resolveRange(s *ast.RangeStmt) ast.Node {
	if s.Value != nil {
		return s.Value
	}
	if s.Key != nil {
		return s.Key
	}
	return s.X
}

func resolveFor(s *ast.ForStmt) ast.Node {
	if s.Init != nil {
		return resolveStmt(s.Init)
	}
	if s.Cond != nil {
		return s.Cond
	}
	return nil
}

func resolveSwitch(s *ast.SwitchStmt) ast.Node {
	if s.Init != nil {
		return resolveStmt(s.Init)
	}
	if s.Tag != nil {
		return s.Tag
	}
	return nil
}

func resolveTypeSwitch(s *ast.TypeSwitchStmt) ast.Node {
	if s.Init != nil {
		return resolveStmt(s.Init)
	}
	return s.Assign
}

func resolveClause(n ast.Node) ast.Node {
	switch s := n.(type) {
	case *ast.CaseClause:
		if len(s.List) > 0 {
			return s.List[0]
		}
		return nil
	case *ast.CommClause:
		if s.Comm != nil {
			return s.Comm
		}
		return nil
	case *ast.LabeledStmt:
		return resolveStmt(s.Stmt)
	}
	return n
}

func declSpec(s *ast.DeclStmt) ast.Node {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return nil
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			return vs
		}
	}
	return nil
}

func joinLockState(a, b lockState) lockState {
	if a == b {
		return a
	}
	return lockFree
}

func stepLock(info *types.Info, deferOps map[ast.Node]bool, k flow.Key, n ast.Node, s lockState) lockState {
	if deferOps[n] {
		return s
	}
	switch lockOpIn(info, k, n) {
	case lockOpLock, lockOpRLock:
		return lockHeld
	case lockOpUnlock, lockOpRUnlock:
		return lockFree
	}
	return s
}

func lockOpIn(info *types.Info, k flow.Key, n ast.Node) string {
	op := ""
	ast.Inspect(n, func(x ast.Node) bool {
		if op != "" || opaqueBody(x) {
			return false
		}
		name, ok := lockOpCall(info, k, x)
		if !ok {
			return true
		}
		op = name
		return false
	})
	return op
}

func opaqueBody(n ast.Node) bool {
	switch n.(type) {
	case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
		return true
	}
	return false
}

func lockOpCall(info *types.Info, k flow.Key, n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	name := sel.Sel.Name
	switch name {
	case lockOpLock, lockOpUnlock, lockOpRLock, lockOpRUnlock:
	default:
		return "", false
	}
	kk, ok := flow.KeyOf(info, sel.X)
	if !ok || kk != k {
		return "", false
	}
	return name, true
}

func lockerKeys(info *types.Info, fn ast.Node) []flow.Key {
	seen := map[flow.Key]bool{}
	var out []flow.Key
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return n == fn
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || !locksOnLocker(info, sel) {
			return true
		}
		k, ok := flow.KeyOf(info, sel.X)
		if !ok || seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return out
}

func locksOnLocker(info *types.Info, sel *ast.SelectorExpr) bool {
	switch sel.Sel.Name {
	case lockOpLock, lockOpUnlock, lockOpRLock, lockOpRUnlock:
	default:
		return false
	}
	t := info.TypeOf(ast.Unparen(sel.X))
	if t == nil {
		return false
	}
	iface := lockerIface()
	return types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)
}

var lockerIface = sync.OnceValue(func() *types.Interface {
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	lock := types.NewFunc(0, nil, lockOpLock, sig)
	unlock := types.NewFunc(0, nil, lockOpUnlock, sig)
	return types.NewInterfaceType([]*types.Func{lock, unlock}, nil).Complete()
})

func deferStmts(fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	body := flow.Body(fn)
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return n == fn
		}
		if _, ok := n.(*ast.DeferStmt); ok {
			out[n] = true
		}
		return true
	})
	return out
}
