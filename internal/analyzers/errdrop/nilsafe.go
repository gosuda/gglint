package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func computeNilSafe(pass *analysis.Pass) map[*types.Func]bool {
	safe := map[*types.Func]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil {
				continue
			}
			obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			if isNilSafeMethod(pass, fn, obj) {
				safe[originOf(obj)] = true
			}
		}
	}
	return safe
}

func isNilSafeMethod(pass *analysis.Pass, fn *ast.FuncDecl, obj *types.Func) bool {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	if _, isPtr := sig.Recv().Type().(*types.Pointer); !isPtr {
		return false
	}
	recv := sig.Recv()
	if recv.Name() == "" {
		return false
	}
	guard, ok := firstIf(fn.Body)
	if !ok {
		return false
	}
	if !endsInReturn(guard.Body) {
		return false
	}
	if mentionsRecv(pass, guard.Body, recv) {
		return false
	}
	return guardsNil(pass, guard.Cond, recv)
}

func firstIf(body *ast.BlockStmt) (*ast.IfStmt, bool) {
	if len(body.List) == 0 {
		return nil, false
	}
	guard, ok := body.List[0].(*ast.IfStmt)
	if !ok {
		return nil, false
	}
	return guard, true
}

func endsInReturn(body *ast.BlockStmt) bool {
	if len(body.List) == 0 {
		return false
	}
	_, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	return ok
}

func guardsNil(pass *analysis.Pass, cond ast.Expr, recv *types.Var) bool {
	cond = ast.Unparen(cond)
	if isNilCompare(pass, cond, recv) {
		return true
	}
	lor, ok := cond.(*ast.BinaryExpr)
	if !ok || lor.Op != token.LOR {
		return false
	}
	return guardsNil(pass, lor.X, recv)
}

func mentionsRecv(pass *analysis.Pass, body *ast.BlockStmt, recv *types.Var) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == recv || pass.TypesInfo.Defs[id] == recv {
			found = true
			return false
		}
		return true
	})
	return found
}

func isNilCompare(pass *analysis.Pass, cond ast.Expr, recv *types.Var) bool {
	b, ok := cond.(*ast.BinaryExpr)
	if !ok || b.Op != token.EQL {
		return false
	}
	return isRecvNil(pass, b.X, b.Y, recv) || isRecvNil(pass, b.Y, b.X, recv)
}

func isRecvNil(pass *analysis.Pass, side, other ast.Expr, recv *types.Var) bool {
	id, ok := ast.Unparen(side).(*ast.Ident)
	if !ok {
		return false
	}
	if pass.TypesInfo.Uses[id] != recv && pass.TypesInfo.Defs[id] != recv {
		return false
	}
	return isNilIdent(pass.TypesInfo, other)
}

func hasNilSafe(pass *analysis.Pass, origin *types.Func, safe map[*types.Func]bool) bool {
	if safe[origin] {
		return true
	}
	var f nilSafe
	return pass.ImportObjectFact(origin, &f)
}
