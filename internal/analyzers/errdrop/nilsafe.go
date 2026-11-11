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
		return true
	}
	if !bodyDerefsRecv(pass, fn.Body, recv) {
		return true
	}
	return hasNilGuard(pass, fn, recv)
}

func hasNilGuard(pass *analysis.Pass, fn *ast.FuncDecl, recv *types.Var) bool {
	guard, ok := firstIf(fn.Body)
	if !ok {
		return false
	}
	if !endsInReturn(guard.Body) {
		return false
	}
	if guard.Init != nil && nodeDerefsRecv(pass, guard.Init, recv) {
		return false
	}
	if bodyDerefsRecv(pass, guard.Body, recv) {
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
	if isNilCompare(pass, ast.Unparen(lor.Y), recv) && !usesRecv(pass, lor.X, recv) {
		return true
	}
	return guardsNil(pass, lor.X, recv)
}

func bodyDerefsRecv(pass *analysis.Pass, body *ast.BlockStmt, recv *types.Var) bool {
	return nodeDerefsRecv(pass, body, recv)
}

func nodeDerefsRecv(pass *analysis.Pass, n ast.Node, recv *types.Var) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if found {
			return false
		}
		switch t := n.(type) {
		case *ast.SelectorExpr:
			if usesRecv(pass, t.X, recv) {
				found = true
				return false
			}
		case *ast.StarExpr:
			if usesRecv(pass, t.X, recv) {
				found = true
				return false
			}
		case *ast.IndexExpr:
			if isRecvIdent(pass, t.X, recv) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func usesRecv(pass *analysis.Pass, e ast.Node, recv *types.Var) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
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

func isRecvIdent(pass *analysis.Pass, e ast.Expr, recv *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && (pass.TypesInfo.Uses[id] == recv || pass.TypesInfo.Defs[id] == recv)
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
