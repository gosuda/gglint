package gorace

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type syncKind int

const (
	kindSend syncKind = iota
	kindRecv
	kindClose
	kindLock
	kindUnlock
	kindWait
	kindDone
	kindNotify
	kindOpaque
)
const waitMethodName = "Wait"

type syncOp struct {
	node     ast.Node
	key      syncOpKey
	kind     syncKind
	deferred bool
}

type syncOpKey struct {
	root types.Object
	path string
}

func (k syncOpKey) same(o syncOpKey) bool {
	return k.root == o.root && k.path == o.path
}

var compatWaitKinds = map[syncKind][]syncKind{
	kindSend:   {kindRecv},
	kindClose:  {kindRecv},
	kindDone:   {kindWait},
	kindNotify: {kindWait},
}

func opaqueEither(sig, wait syncKind) bool {
	return sig == kindOpaque || wait == kindOpaque
}

func recvSendOpen(sig, wait syncKind, knownBuffered bool) bool {
	return sig == kindRecv && wait == kindSend && !knownBuffered
}

func compatible(sig, wait syncKind, knownBuffered bool) bool {
	if opaqueEither(sig, wait) {
		return true
	}
	if recvSendOpen(sig, wait, knownBuffered) {
		return true
	}
	return slices.Contains(compatWaitKinds[sig], wait)
}

func classifyNode(pass *analysis.Pass, s flow.Spawn, n ast.Node) []syncOp {
	switch x := n.(type) {
	case *ast.SendStmt:
		return keyOps(x.Chan, x, kindSend, pass, s)
	case *ast.UnaryExpr:
		if x.Op == token.ARROW {
			return keyOps(x.X, x, kindRecv, pass, s)
		}
	case *ast.RangeStmt:
		return keyOps(x.X, x, kindRecv, pass, s)
	case *ast.CallExpr:
		return classifyCall(pass, s, x)
	}
	return nil
}

func classifyCall(pass *analysis.Pass, s flow.Spawn, call *ast.CallExpr) []syncOp {
	if isCloseCall(pass.TypesInfo, call) {
		if len(call.Args) != 1 {
			return nil
		}
		return keyOps(call.Args[0], call, kindClose, pass, s)
	}
	if k, ok := flow.MethodKey(pass.TypesInfo, call); ok {
		if isSyncObject(pass.TypesInfo, k) {
			return methodOps(call, opKey(pass, s, k))
		}
		return []syncOp{{node: call, key: opKey(pass, s, k), kind: kindOpaque}}
	}
	for _, a := range call.Args {
		key, ok := flow.KeyOf(pass.TypesInfo, a)
		if !ok || !isSyncObject(pass.TypesInfo, key) {
			continue
		}
		return []syncOp{{node: call, key: opKey(pass, s, key), kind: kindOpaque}}
	}
	return nil
}

func methodOps(call *ast.CallExpr, key syncOpKey) []syncOp {
	kind, ok := methodKind(call.Fun)
	if !ok {
		kind = kindOpaque
	}
	return []syncOp{{node: call, key: key, kind: kind}}
}

func methodKind(fun ast.Expr) (syncKind, bool) {
	sel, ok := ast.Unparen(fun).(*ast.SelectorExpr)
	if !ok {
		return kindOpaque, false
	}
	switch sel.Sel.Name {
	case "Lock", "RLock":
		return kindLock, true
	case "Unlock", "RUnlock":
		return kindUnlock, true
	case waitMethodName:
		return kindWait, true
	case "Done":
		return kindDone, true
	case "Signal", "Broadcast":
		return kindNotify, true
	}
	return kindOpaque, false
}

func keyOps(e ast.Expr, n ast.Node, k syncKind, pass *analysis.Pass, s flow.Spawn) []syncOp {
	key, ok := flow.KeyOf(pass.TypesInfo, e)
	if !ok || !isSyncObject(pass.TypesInfo, key) {
		return nil
	}
	return []syncOp{{node: n, key: opKey(pass, s, key), kind: k}}
}

func opKey(pass *analysis.Pass, s flow.Spawn, k flow.Key) syncOpKey {
	r := resolveSpawnParam(pass, s, k)
	return syncOpKey{root: r.Root, path: r.Path}
}

func spawnCallArgs(s flow.Spawn) []ast.Expr {
	if gs, ok := s.Site.(*ast.GoStmt); ok && gs.Call != nil {
		return gs.Call.Args
	}
	return nil
}

func resolveSpawnParam(pass *analysis.Pass, s flow.Spawn, k flow.Key) flow.Key {
	if s.Body == nil || s.Body.Type == nil || s.Body.Type.Params == nil {
		return k
	}
	root, ok := k.Root.(*types.Var)
	if !ok {
		return k
	}
	idx := paramIndex(pass, s.Body, root)
	if idx < 0 {
		return k
	}
	args := spawnCallArgs(s)
	if idx >= len(args) {
		return k
	}
	ak, ok := flow.KeyOf(pass.TypesInfo, args[idx])
	if !ok {
		return k
	}
	return flow.Key{Root: ak.Root, Path: ak.Path + k.Path}
}

func paramIndex(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) int {
	idx := 0
	for _, f := range lit.Type.Params.List {
		for _, n := range f.Names {
			if pass.TypesInfo.Defs[n] == v {
				return idx
			}
			idx++
		}
	}
	return -1
}

func mappedKeys(pass *analysis.Pass, s flow.Spawn, keys []flow.Key) []flow.Key {
	out := make([]flow.Key, 0, len(keys))
	for _, k := range keys {
		out = append(out, resolveSpawnParam(pass, s, k))
	}
	return out
}

func mappedOrder(pass *analysis.Pass, s flow.Spawn, o litOrder) litOrder {
	o.recvBeforeKeys = mappedKeys(pass, s, o.recvBeforeKeys)
	o.recvAfterKeys = mappedKeys(pass, s, o.recvAfterKeys)
	o.sendBeforeKeys = mappedKeys(pass, s, o.sendBeforeKeys)
	o.sendAfterKeys = mappedKeys(pass, s, o.sendAfterKeys)
	return o
}

func bufferedSet(pass *analysis.Pass, fn ast.Node) map[syncOpKey]bool {
	out := map[syncOpKey]bool{}
	for k := range bufferedChanKeys(pass, fn) {
		out[syncOpKey{root: k.Root, path: k.Path}] = true
	}
	return out
}

func collectParentOps(pass *analysis.Pass, fn ast.Node, s flow.Spawn) []syncOp {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []syncOp
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if d, ok := n.(*ast.DeferStmt); ok {
			out = append(out, deferredOps(pass, fn, s, d.Call)...)
			return false
		}
		if gs, ok := n.(*ast.GoStmt); ok {
			out = append(out, goStmtOps(pass, fn, s, gs)...)
			return false
		}
		out = append(out, classifyNode(pass, s, n)...)
		out = append(out, argLitOps(pass, s, n)...)
		out = append(out, funLitOps(pass, s, n)...)
		out = append(out, boundCallOps(pass, fn, s, n)...)
		return true
	})
	return out
}

func goStmtOps(pass *analysis.Pass, fn ast.Node, s flow.Spawn, gs *ast.GoStmt) []syncOp {
	if gs.Call == nil {
		return nil
	}
	var out []syncOp
	for _, a := range gs.Call.Args {
		if _, ok := ast.Unparen(a).(*ast.FuncLit); ok {
			continue
		}
		out = append(out, nestedCallOps(pass, fn, s, a)...)
	}
	return out
}

func nestedCallOps(pass *analysis.Pass, fn ast.Node, s flow.Spawn, root ast.Node) []syncOp {
	var out []syncOp
	ast.Inspect(root, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			out = append(out, classifyNode(pass, s, n)...)
			return true
		}
		out = append(out, classifyNode(pass, s, call)...)
		out = append(out, argLitOps(pass, s, call)...)
		out = append(out, funLitOps(pass, s, call)...)
		out = append(out, boundCallOps(pass, fn, s, call)...)
		return true
	})
	return out
}

func funLitOps(pass *analysis.Pass, s flow.Spawn, n ast.Node) []syncOp {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil
	}
	lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return nil
	}
	var out []syncOp
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		for _, o := range classifyNode(pass, s, x) {
			o.node = ast.Node(call)
			out = append(out, o)
		}
		return true
	})
	return out
}

func collectLitOps(pass *analysis.Pass, fn ast.Node, s flow.Spawn) []syncOp {
	if s.Body == nil || s.Body.Body == nil {
		return nil
	}
	var out []syncOp
	ast.Inspect(s.Body.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if d, ok := n.(*ast.DeferStmt); ok {
			out = append(out, deferredOps(pass, fn, s, d.Call)...)
			return false
		}
		out = append(out, classifyNode(pass, s, n)...)
		out = append(out, argLitOps(pass, s, n)...)
		out = append(out, funLitOps(pass, s, n)...)
		out = append(out, boundCallOps(pass, fn, s, n)...)
		return true
	})
	return out
}

func deferredOps(pass *analysis.Pass, fn ast.Node, s flow.Spawn, call *ast.CallExpr) []syncOp {
	var out []syncOp
	for _, o := range classifyCall(pass, s, call) {
		o.deferred = true
		o.node = ast.Node(call)
		out = append(out, o)
	}
	for _, o := range boundCallOps(pass, fn, s, call) {
		o.deferred = true
		out = append(out, o)
	}
	if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok && lit.Body != nil {
		ast.Inspect(lit.Body, func(x ast.Node) bool {
			if _, ok := x.(*ast.FuncLit); ok {
				return false
			}
			for _, o := range classifyNode(pass, s, x) {
				o.deferred = true
				o.node = ast.Node(call)
				out = append(out, o)
			}
			return true
		})
	}
	return out
}

func argLitOps(pass *analysis.Pass, s flow.Spawn, n ast.Node) []syncOp {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil
	}
	var out []syncOp
	for _, a := range call.Args {
		lit, ok := ast.Unparen(a).(*ast.FuncLit)
		if !ok || lit.Body == nil {
			continue
		}
		ast.Inspect(lit.Body, func(x ast.Node) bool {
			if _, ok := x.(*ast.FuncLit); ok {
				return false
			}
			for _, o := range classifyNode(pass, s, x) {
				o.node = ast.Node(call)
				out = append(out, o)
			}
			return true
		})
	}
	return out
}

func boundCallOps(pass *analysis.Pass, fn ast.Node, s flow.Spawn, n ast.Node) []syncOp {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil
	}
	lit := boundLitSync(pass, fn, call)
	if lit == nil || lit.Body == nil {
		return nil
	}
	var out []syncOp
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		for _, o := range classifyNode(pass, s, x) {
			o.node = ast.Node(call)
			out = append(out, o)
		}
		return true
	})
	return out
}
