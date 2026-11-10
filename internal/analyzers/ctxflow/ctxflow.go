package ctxflow

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzers returns the four context checks of this package.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{rootAnalyzer, cancelAnalyzer, nocontextAnalyzer, donespinAnalyzer}
}

func contextInScope(pass *analysis.Pass, cur inspector.Cursor) bool {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		if hasContextParam(pass.TypesInfo, fn.Node()) {
			return true
		}
	}
	return false
}

func hasContextParam(info *types.Info, fn ast.Node) bool {
	ft := funcTypeOf(fn)
	if ft == nil || ft.Params == nil {
		return false
	}
	for _, field := range ft.Params.List {
		if isContextType(info, field.Type) {
			return true
		}
	}
	return false
}

func funcTypeOf(fn ast.Node) *ast.FuncType {
	switch n := fn.(type) {
	case *ast.FuncDecl:
		return n.Type
	case *ast.FuncLit:
		return n.Type
	}
	return nil
}

func isContextType(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)
	if t == nil {
		return false
	}
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		return obj.Name() == "Context" && obj.Pkg() != nil && obj.Pkg().Path() == "context"
	}
	if ptr, ok := t.(*types.Pointer); ok {
		named, ok := ptr.Elem().(*types.Named)
		if !ok {
			return false
		}
		obj := named.Obj()
		return obj.Name() == "Request" && obj.Pkg() != nil && obj.Pkg().Path() == "net/http"
	}
	return false
}

func inMainOrInit(cur inspector.Cursor) bool {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil)) {
		if d, ok := fn.Node().(*ast.FuncDecl); ok {
			if d.Recv == nil && (d.Name.Name == "main" || d.Name.Name == "init") {
				return true
			}
		}
	}
	return false
}

func stripParens(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func identResolvesTo(info *types.Info, e ast.Expr, v *types.Var) bool {
	id, ok := stripParens(e).(*ast.Ident)
	if !ok {
		return false
	}
	if obj, ok := info.Defs[id]; ok && obj != nil {
		return obj == v
	}
	return info.Uses[id] == v
}

func isNilIdent(info *types.Info, e ast.Expr) bool {
	id, ok := stripParens(e).(*ast.Ident)
	if !ok || id.Name != "nil" {
		return false
	}
	return info.Types[id].IsNil()
}

func varOf(info *types.Info, id *ast.Ident) *types.Var {
	if obj, ok := info.Defs[id]; ok && obj != nil {
		v, _ := obj.(*types.Var)
		return v
	}
	v, _ := info.Uses[id].(*types.Var)
	return v
}

func isContextParam(pass *analysis.Pass, cur inspector.Cursor, v *types.Var) bool {
	info := pass.TypesInfo
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		ft := funcTypeOf(fn.Node())
		if ft == nil || ft.Params == nil {
			continue
		}
		for _, field := range ft.Params.List {
			if !isContextType(info, field.Type) {
				continue
			}
			for _, name := range field.Names {
				if info.Defs[name] == v {
					return true
				}
			}
		}
	}
	return false
}

func ownerOf(cur inspector.Cursor) inspector.Cursor {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		return fn
	}
	return inspector.Cursor{}
}
