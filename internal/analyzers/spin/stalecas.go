package spin

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var stalecasAnalyzer = &analysis.Analyzer{
	Name:     "stalecas",
	Doc:      "report compare-and-swap loops retrying with a stale value; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runStalecas,
}

func runStalecas(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkStalecas(pass, cur, loop)
	})
	return nil, nil
}

func checkStalecas(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) {
	old, ok := staleShape(pass, loop)
	if !ok {
		return
	}
	fn := enclosingFunc(cur)
	if !isLocalVar(fn, old) || old.Pos() >= loop.Pos() {
		return
	}
	if assignsRoot(pass.TypesInfo, loop.Body, old) {
		return
	}
	if loop.Post != nil && assignsRoot(pass.TypesInfo, loop.Post, old) {
		return
	}
	pass.Reportf(loop.Pos(), "after one failed swap %s is stale, so every retry fails: load it again inside the loop", old.Name())
}

func staleShape(pass *analysis.Pass, loop *ast.ForStmt) (*types.Var, bool) {
	if cond, ok := ast.Unparen(loop.Cond).(*ast.UnaryExpr); ok && cond.Op == token.NOT {
		return casOld(pass.TypesInfo, cond.X)
	}
	if loop.Body == nil || len(loop.Body.List) == 0 {
		return nil, false
	}
	check, ok := loop.Body.List[0].(*ast.IfStmt)
	if !ok || check.Init != nil || check.Else != nil {
		return nil, false
	}
	old, ok := casOld(pass.TypesInfo, check.Cond)
	if !ok || !exitsLoop(check) {
		return nil, false
	}
	return old, true
}

func exitsLoop(check *ast.IfStmt) bool {
	if len(check.Body.List) != 1 {
		return false
	}
	switch t := check.Body.List[0].(type) {
	case *ast.BranchStmt:
		return t.Tok == token.BREAK && t.Label == nil
	case *ast.ReturnStmt:
		return true
	default:
		return false
	}
}

func casOld(info *types.Info, e ast.Expr) (*types.Var, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	var old ast.Expr
	switch len(call.Args) {
	case 3:
		fn, ok := info.ObjectOf(sel.Sel).(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "sync/atomic" {
			return nil, false
		}
		if !strings.HasPrefix(fn.Name(), "CompareAndSwap") {
			return nil, false
		}
		old = call.Args[1]
	case 2:
		if !isPkgMethod(info, sel, "sync/atomic", "CompareAndSwap") {
			return nil, false
		}
		old = call.Args[0]
	default:
		return nil, false
	}
	id, ok := ast.Unparen(old).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	return v, true
}

func assignsRoot(info *types.Info, n ast.Node, v *types.Var) bool {
	hit := false
	walkNoLit(n, func(x ast.Node) bool {
		switch t := x.(type) {
		case *ast.AssignStmt:
			for _, lhs := range t.Lhs {
				if hitRoot(info, lhs, v) {
					hit = true
				}
			}
		case *ast.IncDecStmt:
			if hitRoot(info, t.X, v) {
				hit = true
			}
		case *ast.RangeStmt:
			if t.Tok == token.ASSIGN && (hitRoot(info, t.Key, v) || hitRoot(info, t.Value, v)) {
				hit = true
			}
		}
		return !hit
	})
	return hit
}

func hitRoot(info *types.Info, e ast.Expr, v *types.Var) bool {
	id := rootIdent(e)
	if id == nil {
		return false
	}
	return info.ObjectOf(id) == types.Object(v)
}
