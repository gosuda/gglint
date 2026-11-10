package alias

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the four alias checks.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{appendclobber, mapcopy, rangemutate, rangeref}
}

var appendclobber = &analysis.Analyzer{
	Name:     "appendclobber",
	Doc:      "Reports an append through b := a[:n] that overwrites local a read later; needs constant n and capacity proof. It reports when a clobbered element is read here or the slice leaves the function after the append, and it does not follow a local alias.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runAppendclobber,
}

var mapcopy = &analysis.Analyzer{
	Name:     "mapcopy",
	Doc:      "Reports a field write to v := m[k] lost when v never used as a whole; only :=, struct elements.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runMapcopy,
}

var rangemutate = &analysis.Analyzer{
	Name:     "rangemutate",
	Doc:      "Reports s = append(s, ...) in a range over local slice s that reaches the next iteration; silent if body assigns s.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runRangemutate,
}

var rangeref = &analysis.Analyzer{
	Name:     "rangeref",
	Doc:      "Reports &v of range copy v that escapes by append, send, map store, or written outer store; only local slices.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runRangeref,
}

func eachFunc(pass *analysis.Pass, fn func(ast.Node)) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		n := cur.Node()
		if flow.Body(n) == nil {
			continue
		}
		fn(n)
	}
}

func varOf(info *types.Info, id *ast.Ident) (*types.Var, bool) {
	if v, ok := info.Defs[id].(*types.Var); ok {
		return v, true
	}
	if v, ok := info.Uses[id].(*types.Var); ok {
		return v, true
	}
	return nil, false
}

func identVar(info *types.Info, e ast.Expr) (*types.Var, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil, false
	}
	return varOf(info, id)
}

func isAppend(info *types.Info, e ast.Expr) (*ast.CallExpr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil, false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return nil, false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	if !ok || b.Name() != "append" {
		return nil, false
	}
	return call, true
}

func passesPointer(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if _, ok := e.(*ast.StarExpr); ok {
		return true
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if passesPointer(info, sel.X) {
		return true
	}
	if hasPointerHop(info, sel) {
		return true
	}
	t := info.TypeOf(sel.X)
	if t == nil {
		return false
	}
	if _, ok := t.(*types.Pointer); ok {
		return true
	}
	if _, ok := t.Underlying().(*types.Pointer); ok {
		return true
	}
	return false
}

func hasPointerHop(info *types.Info, sel *ast.SelectorExpr) bool {
	s := info.Selections[sel]
	if s == nil {
		return false
	}
	idx := s.Index()
	if len(idx) <= 1 {
		return false
	}
	t := s.Recv()
	for _, i := range idx[:len(idx)-1] {
		st, ok := derefStruct(t)
		if !ok || i < 0 || i >= st.NumFields() {
			return false
		}
		ft := st.Field(i).Type()
		if _, ok := ft.(*types.Pointer); ok {
			return true
		}
		t = ft
	}
	return false
}

func derefStruct(t types.Type) (*types.Struct, bool) {
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	if t == nil {
		return nil, false
	}
	st, ok := t.Underlying().(*types.Struct)
	return st, ok
}
