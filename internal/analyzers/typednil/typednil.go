package typednil

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

var Analyzer = &analysis.Analyzer{
	Name: "typednil",
	Doc: `report typed nil conversions returned as interface results

A return of (*T)(nil), []T(nil), map[K]V(nil), (chan T)(nil), or (func())(nil)
into an interface result stores a typed nil: the interface is never nil, so a
caller's err != nil or x != nil check passes on it.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	in := inspection.Of(pass)
	for cur := range in.Root().Preorder((*ast.ReturnStmt)(nil)) {
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
		switch n := c.Node().(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return signatureOf(info, n)
		}
	}
	return nil
}

func signatureOf(info *types.Info, n ast.Node) *types.Signature {
	switch n := n.(type) {
	case *ast.FuncDecl:
		if fn, ok := info.Defs[n.Name].(*types.Func); ok {
			return fn.Signature()
		}
	case *ast.FuncLit:
		if sig, ok := info.TypeOf(n).(*types.Signature); ok {
			return sig
		}
	}
	return nil
}

func checkResult(pass *analysis.Pass, e ast.Expr, slot types.Type) {
	if !isInterfaceSlot(slot) {
		return
	}
	typ := nilConversion(pass.TypesInfo, e)
	if typ == nil {
		return
	}
	qual := qualifier(pass.Pkg)
	pass.Report(analysis.Diagnostic{
		Pos: e.Pos(),
		End: e.End(),
		Message: fmt.Sprintf("returns typed nil %s as interface %s: the result is never nil; return nil",
			types.TypeString(typ, qual), types.TypeString(slot, qual)),
	})
}

func isInterfaceSlot(t types.Type) bool {
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return false
	}
	return types.IsInterface(t)
}

func nilConversion(info *types.Info, e ast.Expr) types.Type {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !info.Types[call.Fun].IsType() || !isNil(info, call.Args[0]) {
		return nil
	}
	typ := info.TypeOf(call)
	if typ == nil || !isNilable(typ) {
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

func isNilable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature:
		return true
	}
	return false
}

func qualifier(pkg *types.Package) types.Qualifier {
	return func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		return p.Name()
	}
}
