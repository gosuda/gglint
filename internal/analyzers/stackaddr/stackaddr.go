// Package stackaddr reports stack variable addresses kept as uintptr.
package stackaddr

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name: "stackaddr",
	Doc: `report addresses of stack variables kept as uintptr

uintptr(unsafe.Pointer(&x)) with x a local variable or parameter turns a frame
address into an integer. The runtime moves goroutine stacks and does not update
integers, so the value is only safe as a direct argument of a function call,
where the compiler keeps the referent alive and in place.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	in := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for cur := range in.Root().Preorder((*ast.CallExpr)(nil)) {
		conv := cur.Node().(*ast.CallExpr)
		v := stackVariable(pass.TypesInfo, conv)
		if v == nil || isCallArgument(pass.TypesInfo, cur) {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:     conv.Pos(),
			End:     conv.End(),
			Message: "address of stack variable " + v.Name() + " kept as uintptr: the stack can move; convert inside the call expression",
		})
	}
	return nil, nil
}

func stackVariable(info *types.Info, conv *ast.CallExpr) *types.Var {
	if !isConversionTo(info, conv, types.Uintptr) {
		return nil
	}
	ptr, ok := ast.Unparen(conv.Args[0]).(*ast.CallExpr)
	if !ok || !isConversionTo(info, ptr, types.UnsafePointer) {
		return nil
	}
	addr, ok := ast.Unparen(ptr.Args[0]).(*ast.UnaryExpr)
	if !ok || addr.Op != token.AND {
		return nil
	}
	id, ok := ast.Unparen(addr.X).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := info.Uses[id].(*types.Var)
	if !ok || !isFrameVariable(v) {
		return nil
	}
	return v
}

func isConversionTo(info *types.Info, call *ast.CallExpr, kind types.BasicKind) bool {
	if len(call.Args) != 1 || !info.Types[call.Fun].IsType() {
		return false
	}
	typ := info.TypeOf(call)
	if typ == nil {
		return false
	}
	basic, ok := typ.Underlying().(*types.Basic)
	return ok && basic.Kind() == kind
}

func isFrameVariable(v *types.Var) bool {
	switch v.Kind() {
	case types.LocalVar, types.ParamVar, types.RecvVar, types.ResultVar:
		return true
	}
	return false
}

func isCallArgument(info *types.Info, cur inspector.Cursor) bool {
	for cur.ParentEdgeKind() == edge.ParenExpr_X {
		cur = cur.Parent()
	}
	if cur.ParentEdgeKind() != edge.CallExpr_Args {
		return false
	}
	fun := info.Types[cur.Parent().Node().(*ast.CallExpr).Fun]
	return !fun.IsType() && !fun.IsBuiltin()
}
