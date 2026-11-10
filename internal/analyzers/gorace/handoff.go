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
	recvBefore        bool
	loadBefore        bool
	recvAfter         bool
	sendAfter         bool
	closeAfter        bool
	unlockAfter       bool
	storeAfter        bool
	unlockAfterKeys   []flow.Key
	recvBeforeKeys    []flow.Key
	recvBeforeUnknown bool
	sendAfterKeys     []flow.Key
	sendAfterUnknown  bool
	recvAfterKeys     []flow.Key
	recvAfterUnknown  bool
	sendBeforeKeys    []flow.Key
	closeAfterKeys    []flow.Key
	helperSendKeys    []flow.Key
}

func scanLitOrder(pass *analysis.Pass, lit *ast.FuncLit, accessPos token.Pos) litOrder {
	var o litOrder
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != ast.Node(lit.Body) {
			return scanCallArgLit(pass, n, accessPos, &o)
		}
		visitLitNode(pass, n, accessPos, &o)
		return true
	})
	scanChanneledDecl(pass, lit, accessPos, &o)
	return o
}

func scanCallArgLit(pass *analysis.Pass, n ast.Node, accessPos token.Pos, o *litOrder) bool {
	lit, ok := n.(*ast.FuncLit)
	if !ok {
		return true
	}
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		visitLitNodeForArg(pass, x, accessPos, o)
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
		recordRecv(pass.TypesInfo, x.X, x.Pos(), accessPos, o)
	case *ast.SendStmt:
		recordSend(pass, x.Chan, x.Pos(), accessPos, o)
	case *ast.CallExpr:
		visitLitCall(pass, x, accessPos, o)
	}
}

func recordRecv(info *types.Info, e ast.Expr, pos, accessPos token.Pos, o *litOrder) {
	if pos < accessPos {
		o.recvBefore = true
		if k, ok := flow.KeyOf(info, e); ok {
			o.recvBeforeKeys = append(o.recvBeforeKeys, k)
		} else {
			o.recvBeforeUnknown = true
		}
		return
	}
	if pos > accessPos {
		o.recvAfter = true
		if k, ok := flow.KeyOf(info, e); ok {
			o.recvAfterKeys = append(o.recvAfterKeys, k)
		} else {
			o.recvAfterUnknown = true
		}
	}
}

func visitLitNodeForArg(pass *analysis.Pass, x ast.Node, accessPos token.Pos, o *litOrder) {
	switch n := x.(type) {
	case *ast.UnaryExpr:
		argRecvAfter(pass, n, accessPos, o)
	case *ast.SendStmt:
		argSendAfter(pass, n, accessPos, o)
	case *ast.CallExpr:
		argCloseAfter(pass, n, accessPos, o)
	}
}

func argRecvAfter(pass *analysis.Pass, n *ast.UnaryExpr, accessPos token.Pos, o *litOrder) {
	if n.Op != token.ARROW || n.Pos() <= accessPos {
		return
	}
	o.recvAfter = true
	if k, ok := flow.KeyOf(pass.TypesInfo, n.X); ok {
		o.recvAfterKeys = append(o.recvAfterKeys, k)
	} else {
		o.recvAfterUnknown = true
	}
}

func argSendAfter(pass *analysis.Pass, n *ast.SendStmt, accessPos token.Pos, o *litOrder) {
	if n.Pos() <= accessPos {
		return
	}
	o.sendAfter = true
	if k, ok := flow.KeyOf(pass.TypesInfo, n.Chan); ok {
		o.sendAfterKeys = append(o.sendAfterKeys, k)
	} else {
		o.sendAfterUnknown = true
	}
}

func argCloseAfter(pass *analysis.Pass, n *ast.CallExpr, accessPos token.Pos, o *litOrder) {
	if !isCloseCall(pass.TypesInfo, n) || n.Pos() <= accessPos {
		return
	}
	o.closeAfter = true
	if len(n.Args) != 1 {
		return
	}
	if k, ok := flow.KeyOf(pass.TypesInfo, n.Args[0]); ok {
		o.closeAfterKeys = append(o.closeAfterKeys, k)
	}
}

func recordSend(pass *analysis.Pass, e ast.Expr, pos, accessPos token.Pos, o *litOrder) {
	if pos < accessPos {
		if k, ok := flow.KeyOf(pass.TypesInfo, e); ok {
			o.sendBeforeKeys = append(o.sendBeforeKeys, k)
		}
		return
	}
	if pos > accessPos {
		o.sendAfter = true
		if k, ok := flow.KeyOf(pass.TypesInfo, e); ok {
			o.sendAfterKeys = append(o.sendAfterKeys, k)
		} else {
			o.sendAfterUnknown = true
		}
	}
}

func visitLitCall(pass *analysis.Pass, call *ast.CallExpr, accessPos token.Pos, o *litOrder) {
	info := pass.TypesInfo
	if markCloseAfter(info, call, accessPos, o) {
		return
	}
	name := selName(call.Fun)
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
		if len(call.Args) == 1 {
			if k, ok := flow.KeyOf(info, call.Args[0]); ok {
				o.closeAfterKeys = append(o.closeAfterKeys, k)
			}
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
			if k, ok := flow.MethodKey(pass.TypesInfo, call); ok {
				o.helperSendKeys = append(o.helperSendKeys, k)
			} else {
				o.sendAfterUnknown = true
			}
		}
		return true
	})
}

func declReceives(pass *analysis.Pass, call *ast.CallExpr) bool {
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
	return bodyReceives(decl.Body)
}

func bodyReceives(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
			found = true
			return false
		}
		return true
	})
	return found
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

func fanoutOrdered(pass *analysis.Pass, fn ast.Node, s flow.Spawn, accessPos token.Pos, buffered map[flow.Key]bool) bool {
	if s.Body == nil {
		return false
	}
	o := scanLitOrder(pass, s.Body, accessPos)
	if o.loadBefore && o.storeAfter {
		return true
	}
	if recvBeforeUnbuffered(o, buffered) && sendAfterUnbuffered(o, buffered) {
		return true
	}
	for _, k := range o.sendBeforeKeys {
		if slices.Contains(o.recvAfterKeys, k) && chanCapIsOne(pass, fn, k) {
			return true
		}
	}
	return false
}

func recvBeforeUnbuffered(o litOrder, buffered map[flow.Key]bool) bool {
	if o.recvBeforeUnknown {
		return true
	}
	for _, k := range o.recvBeforeKeys {
		if !buffered[k] {
			return true
		}
	}
	return false
}

func sendAfterUnbuffered(o litOrder, buffered map[flow.Key]bool) bool {
	if o.sendAfterUnknown {
		return true
	}
	for _, k := range o.sendAfterKeys {
		if !buffered[k] {
			return true
		}
	}
	return false
}

func chanCapIsOne(pass *analysis.Pass, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && ast.Node(lit.Body) != ast.Node(body) {
			if fnLit, ok := fn.(*ast.FuncLit); !ok || lit != fnLit {
				return false
			}
		}
		v, ok := capOfMake(pass.TypesInfo, n, k)
		if ok && v == 1 {
			found = true
			return false
		}
		return true
	})
	return found
}

func capOfMake(info *types.Info, n ast.Node, k flow.Key) (int64, bool) {
	lhs, rhs, ok := capOperands(n)
	if !ok {
		return 0, false
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok {
		return 0, false
	}
	v, ok := chanCapConst(info, call)
	if !ok {
		return 0, false
	}
	id, ok := identVar(info, lhs)
	if !ok {
		return 0, false
	}
	if got := (flow.Key{Root: id}); got != k {
		return 0, false
	}
	return v, true
}

func capOperands(n ast.Node) (lhs, rhs ast.Expr, ok bool) {
	switch t := n.(type) {
	case *ast.AssignStmt:
		if len(t.Lhs) != 1 || len(t.Rhs) != 1 {
			return nil, nil, false
		}
		return t.Lhs[0], t.Rhs[0], true
	case *ast.ValueSpec:
		if len(t.Names) != 1 || len(t.Values) != 1 {
			return nil, nil, false
		}
		return t.Names[0], t.Values[0], true
	}
	return nil, nil, false
}

func chanCapConst(info *types.Info, call *ast.CallExpr) (int64, bool) {
	if !isMakeBuiltin(info, call) {
		return 0, false
	}
	if len(call.Args) != 2 {
		return 0, false
	}
	if _, isChan := ast.Unparen(call.Args[0]).(*ast.ChanType); !isChan {
		return 0, false
	}
	tv, present := info.Types[call.Args[1]]
	if !present || tv.Value == nil {
		return 0, false
	}
	v, isInt := constant.Int64Val(tv.Value)
	if !isInt {
		return 0, false
	}
	return v, true
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

func (m *sharedScope) handoff(s flow.Spawn, p varAccess, po pairOrder) bool {
	if s.Body == nil {
		return false
	}
	o := po.order
	return m.mutexHandoff(s, p.stmt, o) || m.chanHandoff(s, p.stmt, po.buffered, o) ||
		m.helperHandoff(s, p.stmt, o) || m.closeHandoff(s, p.stmt, o)
}

func (m *sharedScope) mutexHandoff(s flow.Spawn, at ast.Node, o litOrder) bool {
	if !o.unlockAfter {
		return false
	}
	for _, k := range o.unlockAfterKeys {
		if parentHoldsAtSpawn(m.pass, m.fn, s, []flow.Key{k}) && m.locksBetween(s, at, k) {
			return true
		}
	}
	return false
}

func (m *sharedScope) chanHandoff(s flow.Spawn, at ast.Node, buffered map[flow.Key]bool, o litOrder) bool {
	for _, k := range o.recvAfterKeys {
		if m.sendsBetween(buffered, s, at, k) {
			return true
		}
	}
	for _, k := range o.sendAfterKeys {
		if m.recvsBetween(s, at, k) {
			return true
		}
	}
	return false
}

func (m *sharedScope) helperHandoff(s flow.Spawn, at ast.Node, o litOrder) bool {
	for _, k := range o.helperSendKeys {
		if m.helperRecvsBetween(s, at, k) {
			return true
		}
	}
	return false
}

func (m *sharedScope) closeHandoff(s flow.Spawn, at ast.Node, o litOrder) bool {
	for _, k := range o.closeAfterKeys {
		if m.recvsBetween(s, at, k) {
			return true
		}
	}
	return false
}

func (m *sharedScope) sendsBetween(buffered map[flow.Key]bool, s flow.Spawn, at ast.Node, k flow.Key) bool {
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool {
		return nodeSendsOn(m.pass, buffered, n, k)
	})
}

func nodeSendsOn(pass *analysis.Pass, buffered map[flow.Key]bool, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		send, ok := x.(*ast.SendStmt)
		if !ok {
			return true
		}
		ck, ok := flow.KeyOf(pass.TypesInfo, send.Chan)
		if !ok || ck != k {
			return true
		}
		if buffered[k] {
			return true
		}
		found = true
		return false
	})
	return found
}

func (m *sharedScope) recvsBetween(s flow.Spawn, at ast.Node, k flow.Key) bool {
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool {
		return nodeRecvsOn(m.pass, n, k)
	})
}

func nodeRecvsOn(pass *analysis.Pass, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		u, ok := x.(*ast.UnaryExpr)
		if !ok || u.Op != token.ARROW {
			return true
		}
		ck, ok := flow.KeyOf(pass.TypesInfo, u.X)
		if !ok || ck != k {
			return true
		}
		found = true
		return false
	})
	return found
}

func (m *sharedScope) helperRecvsBetween(s flow.Spawn, at ast.Node, k flow.Key) bool {
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool {
		return nodeHelperRecvsOn(m.pass, n, k)
	})
}

func nodeHelperRecvsOn(pass *analysis.Pass, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		rk, ok := flow.MethodKey(pass.TypesInfo, call)
		if !ok || rk != k {
			return true
		}
		if declReceives(pass, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func (m *sharedScope) locksBetween(s flow.Spawn, at ast.Node, k flow.Key) bool {
	return !m.graph.Reaches(s.Site, at, func(n ast.Node) bool {
		return nodeLocksOn(m.pass, n, k)
	})
}

func nodeLocksOn(pass *analysis.Pass, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isLockCallName(selName(call.Fun)) {
			return true
		}
		rk, ok := flow.MethodKey(pass.TypesInfo, call)
		if !ok || rk != k {
			return true
		}
		found = true
		return false
	})
	return found
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
