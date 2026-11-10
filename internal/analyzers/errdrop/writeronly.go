package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

type writerOnly struct {
	Param int
}

func (*writerOnly) AFact() {}

func computeWriterOnly(pass *analysis.Pass) map[*types.Func]int {
	out := map[*types.Func]int{}
	decls, objs := collectNeverFailsCandidates(pass)
	changed := true
	for changed {
		changed = false
		for i, fd := range decls {
			origin := originOf(objs[i])
			if _, done := out[origin]; done {
				continue
			}
			if idx, ok := writerOnlyParam(pass, fd, objs[i], out); ok {
				out[origin] = idx
				changed = true
			}
		}
	}
	return out
}

func writerOnlyParam(pass *analysis.Pass, fd *ast.FuncDecl, obj *types.Func, set map[*types.Func]int) (int, bool) {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Params() == nil || !hasErrorResult(sig) {
		return 0, false
	}
	for _, idx := range writerParamIndexes(sig) {
		pv := sig.Params().At(idx)
		if returnsWriterOnly(pass, fd, sig, pv, set) {
			return idx, true
		}
	}
	return 0, false
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

func returnsWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, sig *types.Signature, pv *types.Var, set map[*types.Func]int) bool {
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

func returnWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, sig *types.Signature, r *ast.ReturnStmt, pv *types.Var, set map[*types.Func]int) bool {
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

func resultWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, e ast.Expr, pv *types.Var, set map[*types.Func]int) bool {
	u := ast.Unparen(e)
	if isNilIdent(pass.TypesInfo, u) || !isError(pass.TypesInfo.TypeOf(u)) {
		return true
	}
	if c := callOf(u); c != nil {
		return callResultWriterOnly(pass, c, pv, set)
	}
	id, isIdent := u.(*ast.Ident)
	if !isIdent || pass.TypesInfo.ObjectOf(id) == types.Object(pv) {
		return false
	}
	return assignedFromWriter(pass, fd, id, pv, set)
}

func callResultWriterOnly(pass *analysis.Pass, c *ast.CallExpr, pv *types.Var, set map[*types.Func]int) bool {
	if !isRealCall(pass.TypesInfo, c) || !slicesHasError(pass, c) {
		return true
	}
	return isWriteOn(pass, c, pv) || forwardsWriter(pass, c, pv, set)
}

func bareReturnWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, sig *types.Signature, pv *types.Var, set map[*types.Func]int) bool {
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

func bareTargetWriterOnly(pass *analysis.Pass, fd *ast.FuncDecl, target, pv *types.Var, set map[*types.Func]int) bool {
	if addressTaken(pass, fd, target) {
		return false
	}
	found := false
	ok := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
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
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
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

func assignedFromWriter(pass *analysis.Pass, fd *ast.FuncDecl, id *ast.Ident, pv *types.Var, set map[*types.Func]int) bool {
	obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return false
	}
	return varAllWriterAssigned(pass, fd, obj, pv, set)
}

func varAllWriterAssigned(pass *analysis.Pass, fd *ast.FuncDecl, obj, pv *types.Var, set map[*types.Func]int) bool {
	found := false
	ok := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
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

func assignAllWriter(pass *analysis.Pass, as *ast.AssignStmt, obj, pv *types.Var, set map[*types.Func]int) bool {
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

func assignElemWriterOnly(pass *analysis.Pass, lhs, rhs ast.Expr, obj, pv *types.Var, set map[*types.Func]int) bool {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(obj) {
		return true
	}
	return rhsWriterOnly(pass, rhs, pv, set)
}

func rhsWriterOnly(pass *analysis.Pass, rhs ast.Expr, pv *types.Var, set map[*types.Func]int) bool {
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
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Write", "WriteString", "WriteByte", "WriteRune":
	default:
		return false
	}
	if !slicesHasError(pass, call) {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(pv)
}

func forwardsWriter(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var, set map[*types.Func]int) bool {
	callee := calleeOrigin(pass, call)
	if callee == nil {
		return false
	}
	q, ok := writerFact(pass, callee, set)
	if !ok || len(call.Args) <= q {
		return false
	}
	sig, ok := callee.Type().(*types.Signature)
	if !ok || sig.Params() == nil || q >= sig.Params().Len() {
		return false
	}
	id, ok := ast.Unparen(call.Args[q]).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(pv)
}

func writerFact(pass *analysis.Pass, origin *types.Func, set map[*types.Func]int) (int, bool) {
	if q, ok := set[origin]; ok {
		return q, true
	}
	var f writerOnly
	if pass.ImportObjectFact(origin, &f) {
		return f.Param, true
	}
	return 0, false
}

func slicesHasError(pass *analysis.Pass, call *ast.CallExpr) bool {
	return slices.ContainsFunc(resultTypes(pass.TypesInfo, call), isError)
}
