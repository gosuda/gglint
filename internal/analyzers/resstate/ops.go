package resstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func hasCloseRelease(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	return isCloseOf(pass, call, v)
}

func isCloseOf(pass *analysis.Pass, call *ast.CallExpr, v *types.Var) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return false
	}
	if len(call.Args) != 0 {
		return false
	}
	k, ok := flow.MethodKey(pass.TypesInfo, call)
	if !ok {
		return false
	}
	return k.Root == v
}

func hasPutRelease(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Put" {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	if !isPoolType(pass.TypesInfo.Types[sel.X].Type) {
		return false
	}
	return exprMentions(pass, call.Args[0], v)
}

func hasFreeRelease(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	if fn.Name() == "_Cfunc_free" {
		return freeArgsMention(pass, call, v)
	}
	pkg := fn.Pkg()
	if pkg == nil {
		return false
	}
	if fn.Name() != "Munmap" {
		return false
	}
	if pkg.Path() != "syscall" && pkg.Path() != "golang.org/x/sys/unix" {
		return false
	}
	return freeArgsMention(pass, call, v)
}

func freeArgsMention(pass *analysis.Pass, call *ast.CallExpr, v *types.Var) bool {
	if len(call.Args) == 0 {
		return false
	}
	return exprMentions(pass, call.Args[0], v)
}

func methodCallsOn(pass *analysis.Pass, n ast.Node, v *types.Var) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, ok := flow.MethodKey(pass.TypesInfo, call)
		if !ok || k.Root != v {
			return true
		}
		if _, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); !ok {
			return true
		}
		out = append(out, call)
		return true
	})
	return out
}

func callName(call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

func assignsVar(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		for _, lhs := range x.Lhs {
			id, ok := ast.Unparen(lhs).(*ast.Ident)
			if !ok {
				continue
			}
			if pass.TypesInfo.Uses[id] == v {
				return true
			}
		}
	case *ast.IncDecStmt:
		id, ok := ast.Unparen(x.X).(*ast.Ident)
		if !ok {
			return false
		}
		return pass.TypesInfo.Uses[id] == v
	case *ast.ValueSpec:
		return false
	}
	return false
}

func mentionsVar(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}

func exprMentions(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	found := false
	ast.Inspect(e, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			found = true
		}
		return !found
	})
	return found
}

func mentionPos(pass *analysis.Pass, n ast.Node, v *types.Var) token.Pos {
	pos := token.NoPos
	ast.Inspect(n, func(x ast.Node) bool {
		if pos != token.NoPos {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if pass.TypesInfo.Uses[id] == v {
			pos = id.Pos()
		}
		return pos == token.NoPos
	})
	if pos != token.NoPos {
		return pos
	}
	return n.Pos()
}

func escapesVar(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	if takesAddr(pass, n, v) {
		return true
	}
	if callArgEscapes(pass, n, v) {
		return true
	}
	if returnEscapes(pass, n, v) {
		return true
	}
	if sendEscapes(pass, n, v) {
		return true
	}
	return storeEscapes(pass, n, v)
}

func takesAddr(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		u, ok := x.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		k, ok := flow.KeyOf(pass.TypesInfo, u.X)
		if ok && k.Root == v {
			found = true
		}
		return !found
	})
	return found
}

func callArgEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
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
		for _, a := range call.Args {
			if exprMentions(pass, a, v) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func returnEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	ret, ok := n.(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, r := range ret.Results {
		if exprMentions(pass, r, v) {
			return true
		}
	}
	return false
}

func sendEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	s, ok := n.(*ast.SendStmt)
	if !ok {
		return false
	}
	return exprMentions(pass, s.Value, v)
}

func storeEscapes(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok {
		return false
	}
	mentioned := false
	for _, r := range as.Rhs {
		if exprMentions(pass, r, v) {
			mentioned = true
		}
	}
	return mentioned
}
