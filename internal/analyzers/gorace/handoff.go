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

type litOrder struct {
	loadBefore        bool
	storeAfter        bool
	recvBeforeKeys    []flow.Key
	recvBeforeUnknown bool
	sendAfterKeys     []flow.Key
	sendAfterUnknown  bool
	sendBeforeKeys    []flow.Key
	recvAfterKeys     []flow.Key
}

func scanLitOrder(pass *analysis.Pass, lit *ast.FuncLit, accessPos token.Pos) litOrder {
	var o litOrder
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != ast.Node(lit.Body) {
			return false
		}
		visitLitNode(pass, n, accessPos, &o)
		return true
	})
	return o
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
		if k, ok := flow.KeyOf(info, e); ok {
			o.recvBeforeKeys = append(o.recvBeforeKeys, k)
		} else {
			o.recvBeforeUnknown = true
		}
		return
	}
	if pos > accessPos {
		if k, ok := flow.KeyOf(info, e); ok {
			o.recvAfterKeys = append(o.recvAfterKeys, k)
		}
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
		if k, ok := flow.KeyOf(pass.TypesInfo, e); ok {
			o.sendAfterKeys = append(o.sendAfterKeys, k)
		} else {
			o.sendAfterUnknown = true
		}
	}
}

func visitLitCall(pass *analysis.Pass, call *ast.CallExpr, accessPos token.Pos, o *litOrder) {
	info := pass.TypesInfo
	if markAtomicBefore(info, call, accessPos, o) {
		return
	}
	markAtomicAfter(info, call, accessPos, o)
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

func chanCapIsOne(pass *analysis.Pass, k flow.Key) bool {
	v, ok := k.Root.(*types.Var)
	if !ok {
		return true
	}
	body := scopeBody(pass, v)
	if body == nil {
		return true
	}
	seen, one := scanScopeCap(pass, body, k)
	return !seen || one
}

func scopeBody(pass *analysis.Pass, v *types.Var) *ast.BlockStmt {
	if v == nil || !v.Pos().IsValid() {
		return nil
	}
	var best *ast.BlockStmt
	for _, f := range pass.Files {
		if v.Pos() < f.Pos() || v.Pos() >= f.End() {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				best = scopeAt(best, lit.Body, v.Pos())
			}
			if decl, ok := n.(*ast.FuncDecl); ok {
				best = scopeAt(best, decl.Body, v.Pos())
			}
			return true
		})
	}
	return best
}

func scopeAt(best, body *ast.BlockStmt, pos token.Pos) *ast.BlockStmt {
	if containsPos(body, pos) {
		return body
	}
	return best
}

func containsPos(body *ast.BlockStmt, pos token.Pos) bool {
	return body != nil && pos >= body.Pos() && pos < body.End()
}

func scanScopeCap(pass *analysis.Pass, body *ast.BlockStmt, k flow.Key) (seen, one bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		if one {
			return false
		}
		if !makesKey(pass.TypesInfo, n, k) {
			return true
		}
		seen = true
		if v, ok := capOfMake(pass.TypesInfo, n, k); ok && v == 1 {
			one = true
			return false
		}
		return true
	})
	return seen, one
}

func makesKey(info *types.Info, n ast.Node, k flow.Key) bool {
	lhs, rhs, ok := capOperands(n)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok || !isMakeBuiltin(info, call) {
		return false
	}
	if _, isChan := ast.Unparen(call.Args[0]).(*ast.ChanType); !isChan {
		return false
	}
	id, ok := identVar(info, lhs)
	if !ok {
		return false
	}
	return flow.Key{Root: id} == k
}

func semaphoreKeys(pass *analysis.Pass, s flow.Spawn, pos token.Pos) []flow.Key {
	if s.Body == nil {
		return nil
	}
	o := mappedOrder(pass, s, scanLitOrder(pass, s.Body, pos))
	var keys []flow.Key
	for _, k := range o.sendBeforeKeys {
		if slices.Contains(o.recvAfterKeys, k) && chanCapIsOne(pass, k) && straightSendAcquire(pass, s.Body, k, pos) {
			keys = append(keys, k)
		}
	}
	return keys
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

func isCloseCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "close"
}

func isWaitCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	path := fn.Pkg().Path()
	return fn.Name() == waitMethodName && (path == "testing/synctest" || strings.HasSuffix(path, "/synctest"))
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
