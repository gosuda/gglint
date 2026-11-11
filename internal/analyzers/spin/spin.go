package spin

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the five spin checks; spin covers these five shapes only.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		pollAnalyzer,
		closedrecvAnalyzer,
		busywaitAnalyzer,
		stalecasAnalyzer,
		skipadvanceAnalyzer,
	}
}

func enclosingFunc(cur inspector.Cursor) ast.Node {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		return fn.Node()
	}
	return nil
}

func isLocalVar(fn ast.Node, v *types.Var) bool {
	if fn == nil || v == nil || v.Kind() != types.LocalVar {
		return false
	}
	return fn.Pos() <= v.Pos() && v.Pos() < fn.End()
}

func walkNoLit(n ast.Node, f func(ast.Node) bool) {
	ast.Inspect(n, func(x ast.Node) bool {
		if x == nil {
			return true
		}
		if _, ok := x.(*ast.FuncLit); ok && x != n {
			return false
		}
		return f(x)
	})
}

func rootIdent(e ast.Expr) *ast.Ident {
	for {
		e = ast.Unparen(e)
		switch t := e.(type) {
		case *ast.Ident:
			return t
		case *ast.SelectorExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.StarExpr:
			e = t.X
		default:
			return nil
		}
	}
}

func isNil(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	id, ok := e.(*ast.Ident)
	if !ok || id.Name != "nil" {
		return false
	}
	if info.Types[e].IsNil() {
		return true
	}
	return info.ObjectOf(id) == types.Universe.Lookup("nil")
}

func isErrorType(t types.Type) bool {
	if t == nil {
		return false
	}
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func isPkgMethod(info *types.Info, sel *ast.SelectorExpr, path, name string) bool {
	s := info.Selections[sel]
	if s == nil {
		return false
	}
	fn, ok := s.Obj().(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == path && fn.Name() == name
}

func forLoops(pass *analysis.Pass, f func(cur inspector.Cursor, loop *ast.ForStmt)) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.ForStmt)(nil)) {
		f(cur, inspection.As[*ast.ForStmt](cur.Node()))
	}
}

func isCodeCall(info *types.Info, call *ast.CallExpr) bool {
	if info != nil && info.Types[call.Fun].IsType() {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return true
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return !ok || (b.Name() != "len" && b.Name() != "cap")
}
