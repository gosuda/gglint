package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

type neverFails struct{}

func (*neverFails) AFact() {}

type nilSafe struct{}

func (*nilSafe) AFact() {}

type facts struct {
	never  map[*types.Func]bool
	safe   map[*types.Func]bool
	sticky map[*types.Func]bool
}

const writeMethodName = "Write"

const nilName = "nil"

// Analyzer reports dropped errors that later code depends on.
var Analyzer = &analysis.Analyzer{
	Name: "errdrop",
	Doc: `reports a dropped error only when later code depends on the call having succeeded

The neverFails fact is derived from the file set of the current build, so a function with build-tagged variants has the fact of the variant that is built.`,
	Requires:  []*analysis.Analyzer{inspect.Analyzer},
	Run:       run,
	FactTypes: []analysis.Fact{(*neverFails)(nil), (*nilSafe)(nil), (*sticky)(nil)},
}

func run(pass *analysis.Pass) (any, error) {
	set := computeNeverFails(pass)
	for origin := range set {
		pass.ExportObjectFact(origin, &neverFails{})
	}
	safe := computeNilSafe(pass)
	for origin := range safe {
		pass.ExportObjectFact(origin, &nilSafe{})
	}
	stickySet := computeSticky(pass)
	for origin := range stickySet {
		pass.ExportObjectFact(origin, &sticky{})
	}
	hash := hashInterface(pass.Pkg)
	for fn := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		processFunc(pass, fn, set, safe, stickySet, hash)
	}
	return nil, nil
}

func isError(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	return named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func isNilIdent(info *types.Info, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || id.Name != nilName {
		return false
	}
	_, ok = info.Uses[id].(*types.Nil)
	return ok
}

func callOf(e ast.Expr) *ast.CallExpr {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil
	}
	return call
}

func isRealCall(info *types.Info, call *ast.CallExpr) bool {
	tv := info.Types[call.Fun]
	return !tv.IsType() && !tv.IsBuiltin()
}

func resultTypes(info *types.Info, call *ast.CallExpr) []types.Type {
	t := info.TypeOf(call)
	tuple, ok := t.(*types.Tuple)
	if !ok {
		return []types.Type{t}
	}
	out := make([]types.Type, tuple.Len())
	for i := range out {
		out[i] = tuple.At(i).Type()
	}
	return out
}

func isBlank(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "_"
}

func owner(cur inspector.Cursor) inspector.Cursor {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		return fn
	}
	return inspector.Cursor{}
}

func processFunc(pass *analysis.Pass, fn inspector.Cursor, set, safe, stickySet map[*types.Func]bool, hash *types.Interface) {
	f := facts{never: set, safe: safe, sticky: stickySet}
	reported := map[token.Pos]bool{}
	checkValueSites(pass, fn, f, hash, reported)
	checkOutSites(pass, fn, f, hash, reported)
	checkDurableSites(pass, fn, f, hash, reported)
	checkVerdictLists(pass, fn, f, hash, reported)
}

func fnBody(fn inspector.Cursor) *ast.BlockStmt {
	switch n := fn.Node().(type) {
	case *ast.FuncDecl:
		return n.Body
	case *ast.FuncLit:
		return n.Body
	}
	return nil
}

func fnSignature(pass *analysis.Pass, fn inspector.Cursor) *types.Signature {
	switch n := fn.Node().(type) {
	case *ast.FuncDecl:
		if obj, ok := pass.TypesInfo.Defs[n.Name].(*types.Func); ok {
			sig, ok := obj.Type().(*types.Signature)
			if !ok {
				return nil
			}
			return sig
		}
	case *ast.FuncLit:
		sig, ok := pass.TypesInfo.TypeOf(n).(*types.Signature)
		if !ok {
			return nil
		}
		return sig
	}
	return nil
}

func declaredHere(fn inspector.Cursor, v *types.Var) bool {
	if v.Kind() != types.LocalVar {
		return false
	}
	body := fnBody(fn)
	if body == nil {
		return false
	}
	return body.Pos() <= v.Pos() && v.Pos() < body.End()
}
