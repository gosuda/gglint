package wgstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func possibleAddCall(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	if passesWG(pass, call, k) {
		return passedWGCounts(pass, call, k)
	}
	return calleeAddsSame(pass, call, k)
}

func passedWGCounts(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return true
	}
	decl := helperDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return true
	}
	return calleeUsesParamForAdd(pass, decl, call, k)
}

func calleeUsesParamForAdd(pass *analysis.Pass, decl *ast.FuncDecl, call *ast.CallExpr, k flow.Key) bool {
	params := paramVars(pass, decl)
	for i, pv := range params {
		if i >= len(call.Args) {
			continue
		}
		rk, ok := flow.KeyOf(pass.TypesInfo, call.Args[i])
		if !ok || rk != k {
			continue
		}
		if paramAddsOrHandsOff(pass, decl, pv) {
			return true
		}
	}
	return false
}

func paramAddsOrHandsOff(pass *analysis.Pass, decl *ast.FuncDecl, pv *types.Var) bool {
	found := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if addsOnParam(pass, decl, call, pv) || passesParam(pass, call, pv) {
			found = true
			return false
		}
		return true
	})
	return found
}

func addsOnParam(pass *analysis.Pass, decl *ast.FuncDecl, call *ast.CallExpr, pv *types.Var) bool {
	ak, _, ok := wgKey(pass.TypesInfo, call, "Add")
	if !ok {
		return false
	}
	if ak.Root == pv {
		return true
	}
	v, ok := ak.Root.(*types.Var)
	return ok && aliasesParam(pass, decl, v, pv, call.Pos()) && isWGPointer(v.Type()) && isWGPointer(pv.Type())
}

func isWGPointer(typ types.Type) bool {
	ptr, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "WaitGroup" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "sync"
}

func aliasesParam(pass *analysis.Pass, decl *ast.FuncDecl, v, pv *types.Var, usePos token.Pos) bool {
	seen := map[*types.Var]bool{}
	cur := v
	before := usePos
	for cur != nil && !seen[cur] {
		seen[cur] = true
		r := latestInitKeyBefore(pass, decl, cur, before)
		if !r.found {
			return false
		}
		if r.key.Root == pv {
			return true
		}
		next, ok := r.key.Root.(*types.Var)
		if !ok || !flow.IsLocal(pass.TypesInfo, decl, next) {
			return false
		}
		cur, before = next, r.pos
	}
	return false
}

type initResult struct {
	key   flow.Key
	pos   token.Pos
	found bool
}

type initScanner struct {
	pass   *analysis.Pass
	target *types.Var
	before token.Pos
	best   initResult
}

func (s *initScanner) step(n ast.Node) bool {
	if _, ok := n.(*ast.GoStmt); ok {
		return false
	}
	if stmt, ok := n.(*ast.AssignStmt); ok {
		s.take(assignInitBefore(s.pass, stmt, s.target, s.before))
		return true
	}
	if spec, ok := n.(*ast.ValueSpec); ok {
		s.take(valueInitBefore(s.pass, spec, s.target, s.before))
	}
	return true
}

func (s *initScanner) take(r initResult) {
	if r.found && (!s.best.found || r.pos > s.best.pos) {
		s.best = r
	}
}

func latestInitKeyBefore(pass *analysis.Pass, decl *ast.FuncDecl, v *types.Var, before token.Pos) initResult {
	scanner := &initScanner{pass: pass, target: v, before: before}
	ast.Inspect(decl.Body, scanner.step)
	return scanner.best
}

func assignInitBefore(pass *analysis.Pass, s *ast.AssignStmt, v *types.Var, before token.Pos) initResult {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 || s.Pos() >= before {
		return initResult{}
	}
	id, ok := s.Lhs[0].(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != v {
		return initResult{}
	}
	k, ok := flow.KeyOf(pass.TypesInfo, s.Rhs[0])
	return initResult{key: k, pos: s.Pos(), found: ok}
}

func valueInitBefore(pass *analysis.Pass, s *ast.ValueSpec, v *types.Var, before token.Pos) initResult {
	if len(s.Names) != 1 || len(s.Values) != 1 || s.Pos() >= before {
		return initResult{}
	}
	if pass.TypesInfo.ObjectOf(s.Names[0]) != v {
		return initResult{}
	}
	k, ok := flow.KeyOf(pass.TypesInfo, s.Values[0])
	return initResult{key: k, pos: s.Pos(), found: ok}
}

func passesParam(pass *analysis.Pass, call *ast.CallExpr, pv *types.Var) bool {
	for _, arg := range call.Args {
		if ak, ok := flow.KeyOf(pass.TypesInfo, arg); ok && ak.Root == pv {
			return true
		}
	}
	return false
}

func passesWG(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	for _, arg := range call.Args {
		ak, ok := flow.KeyOf(pass.TypesInfo, arg)
		if !ok {
			continue
		}
		if ak == k {
			return true
		}
	}
	return false
}

func calleeAddsSame(pass *analysis.Pass, call *ast.CallExpr, k flow.Key) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return false
	}
	decl := helperDecl(pass, fn)
	if decl == nil || decl.Body == nil {
		return false
	}
	return bodyAddsSameForCall(pass, decl, call, k)
}

func bodyAddsSameForCall(pass *analysis.Pass, decl *ast.FuncDecl, call *ast.CallExpr, k flow.Key) bool {
	adds := collectBodyAdds(pass, decl.Body)
	recvVar := receiverVar(pass, decl)
	params := paramVars(pass, decl)
	recvKey := callReceiverKey(pass, call)
	for _, ak := range adds {
		if ak == k && isPackageVar(ak.Root, pass) {
			return true
		}
		if matchesReceiver(recvVar, ak, recvKey, k) {
			return true
		}
		if sameParamAdd(call, params, ak, k, pass) {
			return true
		}
	}
	return false
}

func matchesReceiver(recvVar *types.Var, ak, recvKey, k flow.Key) bool {
	return recvVar != nil && ak.Root == recvVar && recvKey.Root == k.Root && joinPath(recvKey.Path, ak.Path) == k.Path
}

func sameParamAdd(call *ast.CallExpr, params []*types.Var, ak, k flow.Key, pass *analysis.Pass) bool {
	for i, pv := range params {
		if ak.Root != pv || i >= len(call.Args) {
			continue
		}
		rk, ok := flow.KeyOf(pass.TypesInfo, call.Args[i])
		if !ok || rk.Root != k.Root {
			continue
		}
		if joinPath(rk.Path, ak.Path) == k.Path {
			return true
		}
	}
	return false
}

func collectBodyAdds(pass *analysis.Pass, body *ast.BlockStmt) []flow.Key {
	var out []flow.Key
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ak, _, ok := wgKey(pass.TypesInfo, call, "Add"); ok {
			out = append(out, ak)
		}
		return true
	})
	return out
}
