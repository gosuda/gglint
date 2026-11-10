package gorace

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func atomicCallee(info *types.Info, call *ast.CallExpr) (string, bool) {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || fn.Pkg() == nil {
		return "", false
	}
	if fn.Pkg().Path() != "sync/atomic" {
		return "", false
	}
	return fn.Name(), true
}

func isAtomicLoadCAS(info *types.Info, call *ast.CallExpr) bool {
	name, ok := atomicCallee(info, call)
	if !ok {
		return false
	}
	return strings.HasPrefix(name, "Load") || strings.HasPrefix(name, "CompareAndSwap")
}

func isAtomicStoreAddCAS(info *types.Info, call *ast.CallExpr) bool {
	name, ok := atomicCallee(info, call)
	if !ok {
		return false
	}
	return strings.HasPrefix(name, "Store") || strings.HasPrefix(name, "Add") || strings.HasPrefix(name, "CompareAndSwap")
}

func selName(e ast.Expr) string {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

func isLockCallName(name string) bool {
	return name == lockOpLock || name == lockOpRLock
}

func isUnlockCallName(name string) bool {
	return name == lockOpUnlock || name == lockOpRUnlock
}

func isCloseCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "close"
}

type litOrder struct {
	recvBefore      bool
	loadBefore      bool
	lockBefore      bool
	lockBeforeKeys  []flow.Key
	recvAfter       bool
	sendAfter       bool
	closeAfter      bool
	unlockAfter     bool
	storeAfter      bool
	unlockAfterKeys []flow.Key
}

func scanLitOrder(pass *analysis.Pass, lit *ast.FuncLit, accessPos token.Pos) litOrder {
	var o litOrder
	info := pass.TypesInfo
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != ast.Node(lit.Body) {
			return scanCallArgLit(info, n, accessPos, &o)
		}
		visitLitNode(pass, n, accessPos, &o)
		return true
	})
	scanChanneledDecl(pass, lit, accessPos, &o)
	return o
}

func scanCallArgLit(info *types.Info, n ast.Node, accessPos token.Pos, o *litOrder) bool {
	lit, ok := n.(*ast.FuncLit)
	if !ok {
		return true
	}
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		visitLitNodeForArg(info, x, accessPos, o)
		return true
	})
	return false
}

func visitLitNode(pass *analysis.Pass, n ast.Node, accessPos token.Pos, o *litOrder) {
	switch x := n.(type) {
	case *ast.UnaryExpr:
		if x.Op != token.ARROW {
			break
		}
		if x.Pos() < accessPos {
			o.recvBefore = true
		} else if x.Pos() > accessPos {
			o.recvAfter = true
		}
	case *ast.SendStmt:
		if x.Pos() > accessPos {
			o.sendAfter = true
		}
	case *ast.CallExpr:
		visitLitCall(pass, x, accessPos, o)
	}
}

func visitLitNodeForArg(info *types.Info, x ast.Node, accessPos token.Pos, o *litOrder) {
	switch n := x.(type) {
	case *ast.UnaryExpr:
		if n.Op == token.ARROW && n.Pos() > accessPos {
			o.recvAfter = true
		}
	case *ast.SendStmt:
		if n.Pos() > accessPos {
			o.sendAfter = true
		}
	case *ast.CallExpr:
		if isCloseCall(info, n) && n.Pos() > accessPos {
			o.closeAfter = true
		}
	}
}

func visitLitCall(pass *analysis.Pass, call *ast.CallExpr, accessPos token.Pos, o *litOrder) {
	info := pass.TypesInfo
	if markCloseAfter(info, call, accessPos, o) {
		return
	}
	name := selName(call.Fun)
	if markLockBefore(info, name, call, accessPos, o) {
		return
	}
	if markUnlockAfter(info, name, call, accessPos, o) {
		return
	}
	if markAtomicBefore(info, call, accessPos, o) {
		return
	}
	markAtomicAfter(info, call, accessPos, o)
}

func markCloseAfter(info *types.Info, call *ast.CallExpr, accessPos token.Pos, o *litOrder) bool {
	if !isCloseCall(info, call) {
		return false
	}
	if call.Pos() > accessPos {
		o.closeAfter = true
	}
	return true
}

func markLockBefore(info *types.Info, name string, call *ast.CallExpr, accessPos token.Pos, o *litOrder) bool {
	if !isLockCallName(name) {
		return false
	}
	if call.Pos() < accessPos {
		o.lockBefore = true
		if k, ok := flow.MethodKey(info, call); ok {
			o.lockBeforeKeys = append(o.lockBeforeKeys, k)
		}
	}
	return true
}

func markUnlockAfter(info *types.Info, name string, call *ast.CallExpr, accessPos token.Pos, o *litOrder) bool {
	if !isUnlockCallName(name) {
		return false
	}
	if call.Pos() > accessPos {
		o.unlockAfter = true
		if k, ok := flow.MethodKey(info, call); ok {
			o.unlockAfterKeys = append(o.unlockAfterKeys, k)
		}
	}
	return true
}

func markAtomicBefore(info *types.Info, call *ast.CallExpr, accessPos token.Pos, o *litOrder) bool {
	if !isAtomicLoadCAS(info, call) {
		return false
	}
	if call.Pos() < accessPos {
		o.loadBefore = true
	}
	return true
}

func markAtomicAfter(info *types.Info, call *ast.CallExpr, accessPos token.Pos, o *litOrder) {
	if isAtomicStoreAddCAS(info, call) && call.Pos() > accessPos {
		o.storeAfter = true
	}
}

func scanChanneledDecl(pass *analysis.Pass, lit *ast.FuncLit, accessPos token.Pos, o *litOrder) {
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() <= accessPos {
			return true
		}
		if declSignalsChannel(pass, call) {
			o.sendAfter = true
		}
		return true
	})
}

func declSignalsChannel(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || pass.Pkg == nil {
		return false
	}
	if fn.Pkg() != pass.Pkg {
		return false
	}
	decl := funcDeclFor(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	return declBodySignals(pass.TypesInfo, decl.Body)
}

func declBodySignals(info *types.Info, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if nodeSignals(info, n) {
			found = true
			return false
		}
		return true
	})
	return found
}

func nodeSignals(info *types.Info, n ast.Node) bool {
	switch x := n.(type) {
	case *ast.SendStmt:
		return true
	case *ast.CallExpr:
		return isCloseCall(info, x)
	}
	return false
}

func funcDeclFor(pass *analysis.Pass, fn *types.Func) *ast.FuncDecl {
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if pass.TypesInfo.Defs[fd.Name] == fn {
				return fd
			}
		}
	}
	return nil
}

func fanoutOrdered(pass *analysis.Pass, fn ast.Node, s flow.Spawn, accessPos token.Pos) bool {
	if s.Body == nil {
		return false
	}
	o := scanLitOrder(pass, s.Body, accessPos)
	if o.recvBefore || o.loadBefore {
		return true
	}
	if o.lockBefore && parentHoldsAtSpawn(pass, fn, s, o.lockBeforeKeys) {
		return true
	}
	return false
}

func parentHoldsAtSpawn(pass *analysis.Pass, fn ast.Node, s flow.Spawn, keys []flow.Key) bool {
	if len(keys) == 0 {
		return false
	}
	h := newHolder(pass, fn)
	held := h.at(s.Site)
	for _, k := range keys {
		if slices.Contains(held, k) {
			return true
		}
	}
	return false
}

func (m *sharedScope) waitsBetween(buffered map[flow.Key]bool, s flow.Spawn, at ast.Node) bool {
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool {
		return parentWaitNode(m.pass, buffered, n)
	})
}

func parentWaitNode(pass *analysis.Pass, buffered map[flow.Key]bool, n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if nodeWaits(pass, buffered, x) {
			found = true
			return false
		}
		return true
	})
	return found
}

func nodeWaits(pass *analysis.Pass, buffered map[flow.Key]bool, x ast.Node) bool {
	switch t := x.(type) {
	case *ast.UnaryExpr:
		return t.Op == token.ARROW
	case *ast.SendStmt:
		return sendWaits(pass.TypesInfo, buffered, t)
	case *ast.CallExpr:
		return isOpaqueWait(pass.TypesInfo, t) || isLockCallName(selName(t.Fun))
	}
	return false
}

func sendWaits(info *types.Info, buffered map[flow.Key]bool, send *ast.SendStmt) bool {
	if len(buffered) == 0 {
		return true
	}
	k, ok := flow.KeyOf(info, send.Chan)
	if !ok {
		return true
	}
	return !buffered[k]
}

func bufferedChanKeys(pass *analysis.Pass, fn ast.Node) map[flow.Key]bool {
	out := map[flow.Key]bool{}
	body := flow.Body(fn)
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		switch t := n.(type) {
		case *ast.AssignStmt:
			markBufferedAssign(pass.TypesInfo, t, out)
		case *ast.ValueSpec:
			markBufferedValue(pass.TypesInfo, t, out)
		}
		return true
	})
	return out
}

func markBufferedAssign(info *types.Info, asn *ast.AssignStmt, out map[flow.Key]bool) {
	if len(asn.Lhs) != 1 || len(asn.Rhs) != 1 {
		return
	}
	call, ok := ast.Unparen(asn.Rhs[0]).(*ast.CallExpr)
	if !ok || !makeChanCap(info, call) {
		return
	}
	if v, ok := identVar(info, asn.Lhs[0]); ok {
		out[flow.Key{Root: v}] = true
	}
}

func markBufferedValue(info *types.Info, spec *ast.ValueSpec, out map[flow.Key]bool) {
	if len(spec.Names) != 1 || len(spec.Values) != 1 {
		return
	}
	call, ok := ast.Unparen(spec.Values[0]).(*ast.CallExpr)
	if !ok || !makeChanCap(info, call) {
		return
	}
	if v, ok := identVar(info, spec.Names[0]); ok {
		out[flow.Key{Root: v}] = true
	}
}

func identVar(info *types.Info, e ast.Expr) (*types.Var, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil, false
	}
	if v, ok := info.Defs[id].(*types.Var); ok {
		return v, true
	}
	if v, ok := info.Uses[id].(*types.Var); ok {
		return v, true
	}
	return nil, false
}

func makeChanCap(info *types.Info, call *ast.CallExpr) bool {
	if !isMakeBuiltin(info, call) {
		return false
	}
	if len(call.Args) != 2 {
		return false
	}
	if _, isChan := ast.Unparen(call.Args[0]).(*ast.ChanType); !isChan {
		return false
	}
	return chanCapPositive(info, call.Args[1])
}

func isMakeBuiltin(info *types.Info, call *ast.CallExpr) bool {
	id, isIdent := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdent || id.Name != "make" {
		return false
	}
	b, isBuiltin := info.Uses[id].(*types.Builtin)
	return isBuiltin && b.Name() == "make"
}

func chanCapPositive(info *types.Info, e ast.Expr) bool {
	tv, present := info.Types[e]
	if !present || tv.Value == nil {
		return false
	}
	v, isInt := constant.Int64Val(tv.Value)
	return isInt && v > 0
}

func isOpaqueWait(info *types.Info, call *ast.CallExpr) bool {
	if isCloseCall(info, call) {
		return false
	}
	name := selName(call.Fun)
	if isLockCallName(name) || isUnlockCallName(name) {
		return false
	}
	if _, ok := atomicCallee(info, call); ok {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
			_, ok := info.Uses[id].(*types.Builtin)
			return !ok
		}
		return true
	}
	return true
}

func litSignalsAfter(o litOrder) bool {
	return o.recvAfter || o.sendAfter || o.closeAfter || o.unlockAfter || o.storeAfter
}

func (m *sharedScope) handoff(s flow.Spawn, p varAccess, po pairOrder) bool {
	if s.Body == nil {
		return false
	}
	if !litSignalsAfter(po.order) {
		return false
	}
	if !m.waitsBetween(po.buffered, s, p.stmt) {
		return false
	}
	if po.order.unlockAfter && !parentHoldsAtSpawn(m.pass, m.fn, s, po.order.unlockAfterKeys) {
		return false
	}
	return true
}

func isWaitCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	path := fn.Pkg().Path()
	return fn.Name() == "Wait" && (path == "testing/synctest" || strings.HasSuffix(path, "/synctest"))
}

func isWaitViaCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || pass.Pkg == nil {
		return false
	}
	if fn.Pkg() != pass.Pkg {
		return false
	}
	inner, ok := lastCallOf(funcDeclFor(pass, fn))
	if !ok {
		return false
	}
	return isWaitCall(pass.TypesInfo, inner)
}

func lastCallOf(decl *ast.FuncDecl) (*ast.CallExpr, bool) {
	if decl == nil || decl.Body == nil || len(decl.Body.List) == 0 {
		return nil, false
	}
	last := decl.Body.List[len(decl.Body.List)-1]
	expr, ok := last.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	inner, ok := ast.Unparen(expr.X).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	return inner, true
}
