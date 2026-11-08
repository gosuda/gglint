package typednil

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

var Analyzer = &analysis.Analyzer{
	Name: "typednil",
	Doc: `report typed nil pointer conversions returned as interface results

A return of the conversion (*T)(nil) into an interface result stores a typed
nil pointer: the interface is never nil, so a caller's err != nil or x != nil
check passes on it. Other nil conversions, such as []T(nil), are not reported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.ReturnStmt)(nil)) {
		sig := enclosingSignature(pass.TypesInfo, cur)
		ret := inspection.Node[*ast.ReturnStmt](cur)
		if sig == nil || len(ret.Results) != sig.Results().Len() {
			continue
		}
		for i, e := range ret.Results {
			checkResult(pass, e, sig.Results().At(i).Type())
		}
	}
	return nil, nil
}

func enclosingSignature(info *types.Info, cur inspector.Cursor) *types.Signature {
	for c := cur.Parent(); c.Node() != nil; c = c.Parent() {
		var fn ast.Expr
		switch n := c.Node().(type) {
		case *ast.FuncDecl:
			fn = n.Name
		case *ast.FuncLit:
			fn = n
		default:
			continue
		}
		if sig, ok := info.TypeOf(fn).(*types.Signature); ok {
			return sig
		}
		return nil
	}
	return nil
}

func checkResult(pass *analysis.Pass, e ast.Expr, slot types.Type) {
	if _, ok := types.Unalias(slot).(*types.TypeParam); ok || !types.IsInterface(slot) {
		return
	}
	typ := nilConversion(pass.TypesInfo, e)
	if typ == nil {
		return
	}
	qual := qualifier(pass.Pkg)
	pass.ReportRangef(e, "returns typed nil %s as interface %s: the result is never nil; return nil",
		types.TypeString(typ, qual), types.TypeString(slot, qual))
}

func nilConversion(info *types.Info, e ast.Expr) types.Type {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !info.Types[call.Fun].IsType() || !isNil(info, call.Args[0]) {
		return nil
	}
	typ := info.TypeOf(call)
	if _, ok := typ.Underlying().(*types.Pointer); !ok {
		return nil
	}
	return typ
}

func isNil(info *types.Info, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = info.Uses[id].(*types.Nil)
	return ok
}

func qualifier(pkg *types.Package) types.Qualifier {
	return func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		return p.Name()
	}
}
