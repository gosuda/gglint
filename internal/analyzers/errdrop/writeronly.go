package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

type writerOnly struct {
	Param  int
	Write  bool
	String bool
}

func (*writerOnly) AFact() {}

func computeWriterOnly(pass *analysis.Pass) map[*types.Func]writerOnly {
	out := map[*types.Func]writerOnly{}
	decls, objs := collectNeverFailsCandidates(pass)
	changed := true
	for changed {
		changed = false
		for i, fd := range decls {
			origin := originOf(objs[i])
			if _, done := out[origin]; done {
				continue
			}
			if need, ok := writerOnlyParam(pass, fd, objs[i], out); ok {
				out[origin] = need
				changed = true
			}
		}
	}
	return out
}

func writerOnlyParam(pass *analysis.Pass, fd *ast.FuncDecl, obj *types.Func, set map[*types.Func]writerOnly) (writerOnly, bool) {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Params() == nil || !hasErrorResult(sig) {
		return writerOnly{}, false
	}
	for _, idx := range writerParamIndexes(sig) {
		pv := sig.Params().At(idx)
		if returnsWriterOnly(pass, fd, sig, pv, set) {
			w, s := writerNeeds(pass, fd, pv, set)
			return writerOnly{Param: idx, Write: w, String: s}, true
		}
	}
	return writerOnly{}, false
}

func writerParamIndexes(sig *types.Signature) []int {
	var out []int
	for i := range sig.Params().Len() {
		pt := sig.Params().At(i).Type()
		if _, ok := types.Unalias(pt).Underlying().(*types.Interface); ok {
			out = append(out, i)
		}
	}
	return out
}

func returnsWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, sig *types.Signature, pv *types.Var, set map[*types.Func]writerOnly) bool {
	ok := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		r, isRet := n.(*ast.ReturnStmt)
		if !isRet || returnWriterOnly(pass, fd, sig, r, pv, set) {
			return true
		}
		ok = false
		return false
	})
	return ok
}

func returnWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, sig *types.Signature, r *ast.ReturnStmt, pv *types.Var, set map[*types.Func]writerOnly) bool {
	if len(r.Results) == 0 {
		return bareReturnWriterOnly(pass, fd, sig, pv, set)
	}
	for _, e := range r.Results {
		if !resultWriterOnly(pass, fd, e, pv, set) {
			return false
		}
	}
	return true
}

func resultWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, e ast.Expr, pv *types.Var, set map[*types.Func]writerOnly) bool {
	u := ast.Unparen(e)
	if c := callOf(u); c != nil {
		return callResultWriterOnly(pass, c, pv, set)
	}
	if isNilIdent(pass.TypesInfo, u) || !isError(pass.TypesInfo.TypeOf(u)) {
		return true
	}
	id, isIdent := u.(*ast.Ident)
	if !isIdent || pass.TypesInfo.ObjectOf(id) == types.Object(pv) {
		return false
	}
	return assignedFromWriter(pass, fd, id, pv, set)
}

func callResultWriterOnly(pass *analysis.Pass, c *ast.CallExpr, pv *types.Var, set map[*types.Func]writerOnly) bool {
	if !isRealCall(pass.TypesInfo, c) || !slicesHasError(pass, c) {
		return true
	}
	return isWriteOn(pass, c, pv) || forwardsWriter(pass, c, pv, set)
}

func bareReturnWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, sig *types.Signature, pv *types.Var, set map[*types.Func]writerOnly) bool {
	targets := namedErrorTargets(sig)
	if len(targets) == 0 {
		return true
	}
	for _, target := range targets {
		if !bareTargetWriterOnly(pass, fd, target, pv, set) {
			return false
		}
	}
	return true
}

func bareTargetWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, target, pv *types.Var, set map[*types.Func]writerOnly) bool {
	if addressTaken(pass, fd, target) {
		return false
	}
	found := false
	ok := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		as, isAssign := n.(*ast.AssignStmt)
		if !isAssign || !assignTargets(pass, as, target) {
			return true
		}
		found = true
		if !assignAllWriter(pass, as, target, pv, set) {
			ok = false
			return false
		}
		return true
	})
	return !found || ok
}

func addressTaken(pass *analysis.Pass, fd *ast.FuncDecl, target *types.Var) bool {
	taken := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		u, isUnary := n.(*ast.UnaryExpr)
		if !isUnary || u.Op != token.AND {
			return true
		}
		if resolvesTo(pass.TypesInfo, u.X, []*types.Var{target}) {
			taken = true
			return false
		}
		return true
	})
	return taken
}

func assignedFromWriter(pass *analysis.Pass, fd *ast.FuncDecl, id *ast.Ident, pv *types.Var, set map[*types.Func]writerOnly) bool {
	obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return false
	}
	return varAllWriterAssigned(pass, fd, obj, pv, set)
}

func varAllWriterAssigned(pass *analysis.Pass, fd *ast.FuncDecl, obj, pv *types.Var, set map[*types.Func]writerOnly) bool {
	found := false
	ok := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		as, isAssign := n.(*ast.AssignStmt)
		if !isAssign || !assignTargets(pass, as, obj) {
			return true
		}
		found = true
		if !assignAllWriter(pass, as, obj, pv, set) {
			ok = false
			return false
		}
		return true
	})
	return found && ok
}

func assignTargets(pass *analysis.Pass, as *ast.AssignStmt, obj *types.Var) bool {
	for _, lhs := range as.Lhs {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if ok && pass.TypesInfo.ObjectOf(id) == types.Object(obj) {
			return true
		}
	}
	return false
}

func assignAllWriter(pass *analysis.Pass, as *ast.AssignStmt, obj, pv *types.Var, set map[*types.Func]writerOnly) bool {
	for i, lhs := range as.Lhs {
		rhs := as.Rhs[0]
		if len(as.Rhs) == len(as.Lhs) {
			rhs = as.Rhs[i]
		}
		if !assignElemWriterOnly(pass, lhs, rhs, obj, pv, set) {
			return false
		}
	}
	return true
}

func assignElemWriterOnly(pass *analysis.Pass, lhs, rhs ast.Expr, obj, pv *types.Var, set map[*types.Func]writerOnly) bool {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(obj) {
		return true
	}
	return rhsWriterOnly(pass, rhs, pv, set)
}

func rhsWriterOnly(pass *analysis.Pass, rhs ast.Expr, pv *types.Var, set map[*types.Func]writerOnly) bool {
	u := ast.Unparen(rhs)
	if isNilIdent(pass.TypesInfo, u) {
		return true
	}
	c := callOf(u)
	if c == nil || !isRealCall(pass.TypesInfo, c) || !slicesHasError(pass, c) {
		return false
	}
	return isWriteOn(pass, c, pv) || forwardsWriter(pass, c, pv, set)
}

func isWriteOn(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var) bool {
	return isWriteCallOn(pass, call, pv) || isWriteStringOn(pass, call, pv)
}

func isWriteCallOn(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Write", "WriteByte", "WriteRune":
	default:
		return false
	}
	if !slicesHasError(pass, call) {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(pv)
}

func isWriteStringOn(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteString" {
		return false
	}
	if !slicesHasError(pass, call) {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(pv)
}

func writerNeeds(pass *analysis.Pass, fd *ast.FuncDecl, pv *types.Var, set map[*types.Func]writerOnly) (write, str bool) {
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isWriteCallOn(pass, c, pv) {
			write = true
		}
		if isWriteStringOn(pass, c, pv) {
			str = true
		}
		if need, ok := forwardedNeed(pass, c, pv, set); ok {
			write = write || need.Write
			str = str || need.String
		}
		return true
	})
	return write, str
}

func forwardedNeed(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var, set map[*types.Func]writerOnly) (writerOnly, bool) {
	callee := calleeOrigin(pass, call)
	if callee == nil {
		return writerOnly{}, false
	}
	need, ok := writerFact(pass, callee, set)
	if !ok || !forwardArgIsWriter(pass, call, pv, need, callee) {
		return writerOnly{}, false
	}
	return need, true
}

func forwardArgIsWriter(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var, need writerOnly, callee *types.Func) bool {
	if len(call.Args) <= need.Param {
		return false
	}
	sig, ok := callee.Type().(*types.Signature)
	if !ok || sig.Params() == nil || need.Param >= sig.Params().Len() {
		return false
	}
	id, ok := ast.Unparen(call.Args[need.Param]).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(pv)
}

func forwardsWriter(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var, set map[*types.Func]writerOnly) bool {
	_, ok := forwardedNeed(pass, call, pv, set)
	return ok
}

func writerFact(pass *analysis.Pass, origin *types.Func, set map[*types.Func]writerOnly) (writerOnly, bool) {
	if need, ok := set[origin]; ok {
		return need, true
	}
	var f writerOnly
	if pass.ImportObjectFact(origin, &f) {
		return f, true
	}
	return writerOnly{}, false
}

func slicesHasError(pass *analysis.Pass, call *ast.CallExpr) bool {
	return slices.ContainsFunc(resultTypes(pass.TypesInfo, call), isError)
}
