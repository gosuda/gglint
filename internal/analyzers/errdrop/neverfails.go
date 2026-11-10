package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

func originOf(fn *types.Func) *types.Func {
	if o := fn.Origin(); o != nil {
		return o
	}
	return fn
}

func computeNeverFails(pass *analysis.Pass) map[*types.Func]bool {
	decls, objs := collectNeverFailsCandidates(pass)
	return fillNeverFailsSet(pass, decls, objs)
}

func collectNeverFailsCandidates(pass *analysis.Pass) ([]*ast.FuncDecl, []*types.Func) {
	decls := []*ast.FuncDecl{}
	objs := []*types.Func{}
	for _, f := range pass.Files {
		collectFileCandidates(pass, f, &decls, &objs)
	}
	return decls, objs
}

func collectFileCandidates(pass *analysis.Pass, f *ast.File, decls *[]*ast.FuncDecl, objs *[]*types.Func) {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
		if !ok {
			continue
		}
		sig, ok := obj.Type().(*types.Signature)
		if !ok || !hasErrorResult(sig) {
			continue
		}
		*decls = append(*decls, fn)
		*objs = append(*objs, obj)
	}
}

func fillNeverFailsSet(pass *analysis.Pass, decls []*ast.FuncDecl, objs []*types.Func) map[*types.Func]bool {
	set := map[*types.Func]bool{}
	changed := true
	for changed {
		changed = false
		for i, fn := range decls {
			if tryMarkNeverFails(pass, fn, objs[i], set) {
				changed = true
			}
		}
	}
	return set
}

func tryMarkNeverFails(pass *analysis.Pass, fn *ast.FuncDecl, obj *types.Func, set map[*types.Func]bool) bool {
	origin := originOf(obj)
	if set[origin] {
		return false
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig == nil {
		return false
	}
	if !namedResultsOK(pass, fn, sig) {
		return false
	}
	if !returnsOK(pass, fn, sig, set) {
		return false
	}
	set[origin] = true
	return true
}

func hasErrorResult(sig *types.Signature) bool {
	res := sig.Results()
	if res == nil {
		return false
	}
	for v := range res.Variables() {
		if isError(v.Type()) {
			return true
		}
	}
	return false
}

func namedResultsOK(pass *analysis.Pass, fn *ast.FuncDecl, sig *types.Signature) bool {
	targets := namedErrorTargets(sig)
	if len(targets) == 0 {
		return true
	}
	assigned, addressed := scanNamedTargets(pass, fn, targets)
	return !assigned && !addressed
}

func namedErrorTargets(sig *types.Signature) []*types.Var {
	res := sig.Results()
	if res == nil {
		return nil
	}
	targets := []*types.Var{}
	for v := range res.Variables() {
		if !isError(v.Type()) {
			continue
		}
		if v.Name() == "" || v.Name() == "_" {
			continue
		}
		targets = append(targets, v)
	}
	return targets
}

func scanNamedTargets(pass *analysis.Pass, fn *ast.FuncDecl, targets []*types.Var) (assigned, addressed bool) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		noteNamedUse(pass.TypesInfo, n, targets, &assigned, &addressed)
		return !assigned || !addressed
	})
	return assigned, addressed
}

func noteNamedUse(info *types.Info, n ast.Node, targets []*types.Var, assigned, addressed *bool) {
	switch t := n.(type) {
	case *ast.AssignStmt:
		noteAssignUse(info, t, targets, assigned)
	case *ast.UnaryExpr:
		if t.Op == token.AND && resolvesTo(info, t.X, targets) {
			*addressed = true
		}
	case *ast.IncDecStmt:
		if resolvesTo(info, t.X, targets) {
			*assigned = true
		}
	case *ast.RangeStmt:
		if rangeAssigns(info, t, targets) {
			*assigned = true
		}
	}
}

func noteAssignUse(info *types.Info, t *ast.AssignStmt, targets []*types.Var, assigned *bool) {
	for _, lhs := range t.Lhs {
		if resolvesTo(info, lhs, targets) {
			*assigned = true
		}
	}
}

func rangeAssigns(info *types.Info, t *ast.RangeStmt, targets []*types.Var) bool {
	if t.Tok != token.ASSIGN {
		return false
	}
	if t.Key != nil && resolvesTo(info, t.Key, targets) {
		return true
	}
	return t.Value != nil && resolvesTo(info, t.Value, targets)
}

func resolvesTo(info *types.Info, e ast.Expr, targets []*types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	for _, v := range targets {
		if o := info.ObjectOf(id); o == v {
			return true
		}
	}
	return false
}

func returnsOK(pass *analysis.Pass, fn *ast.FuncDecl, sig *types.Signature, set map[*types.Func]bool) bool {
	rets := []*ast.ReturnStmt{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if r, ok := n.(*ast.ReturnStmt); ok {
			rets = append(rets, r)
		}
		return true
	})
	for _, r := range rets {
		if !returnOK(pass, r, sig, set) {
			return false
		}
	}
	return true
}

func returnOK(pass *analysis.Pass, ret *ast.ReturnStmt, sig *types.Signature, set map[*types.Func]bool) bool {
	nres := resultsLen(sig)
	if len(ret.Results) == 0 {
		return allErrorsNamed(sig)
	}
	if len(ret.Results) == 1 && singleCallPasses(pass, ret.Results[0], sig, set) {
		return true
	}
	if len(ret.Results) != nres {
		return false
	}
	for i := range nres {
		if !isError(sig.Results().At(i).Type()) {
			continue
		}
		if !isNilIdent(pass.TypesInfo, ret.Results[i]) {
			return false
		}
	}
	return true
}

func resultsLen(sig *types.Signature) int {
	if sig.Results() == nil {
		return 0
	}
	return sig.Results().Len()
}

func singleCallPasses(pass *analysis.Pass, e ast.Expr, sig *types.Signature, set map[*types.Func]bool) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return resultsLen(sig) == 1 && isNilIdent(pass.TypesInfo, e)
	}
	callee := calleeOrigin(pass, call)
	if callee == nil {
		return resultsLen(sig) == 1 && isNilIdent(pass.TypesInfo, e)
	}
	csig, ok := callee.Type().(*types.Signature)
	if !ok {
		return false
	}
	if resultsLen(csig) != resultsLen(sig) {
		return false
	}
	if !hasFact(pass, callee, set) {
		return false
	}
	return errorPositionsMatch(sig, csig)
}

func errorPositionsMatch(sig, csig *types.Signature) bool {
	for i := range resultsLen(sig) {
		if isError(sig.Results().At(i).Type()) && !isError(csig.Results().At(i).Type()) {
			return false
		}
	}
	return true
}

func allErrorsNamed(sig *types.Signature) bool {
	res := sig.Results()
	if res == nil {
		return true
	}
	for v := range res.Variables() {
		if !isError(v.Type()) {
			continue
		}
		if v.Name() == "" {
			return false
		}
	}
	return true
}

func calleeOrigin(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	obj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || obj == nil {
		return nil
	}
	return originOf(obj)
}

func hasFact(pass *analysis.Pass, origin *types.Func, set map[*types.Func]bool) bool {
	if set[origin] {
		return true
	}
	var f neverFails
	return pass.ImportObjectFact(origin, &f)
}
