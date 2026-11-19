package stackaddr

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	reflectPkg   = "reflect"
	valueType    = "Value"
	sliceHeader  = "SliceHeader"
	stringHeader = "StringHeader"
)

const (
	unsafePointerMisuse = "possible misuse of unsafe.Pointer: uintptr does not keep its referent alive; convert directly in the same expression"
	headerMisuse        = "possible misuse of reflect.%s value; keep a pointer to the original slice or string"
)

var Analyzer = &analysis.Analyzer{
	Name: "stackaddr",
	Doc: `report risky unsafe.Pointer conversions

An unsafe.Pointer built from a uintptr that the conversion does not derive
from a pointer in the same expression may point at freed memory: a uintptr
does not keep its referent alive. Reading through or copying a
reflect.SliceHeader or reflect.StringHeader value has the same flaw; keep a
pointer to the original slice or string instead. Port of loGos
correctness::arena_pointer_escape.`,
	Run: run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			checkNode(pass, n)
			return true
		})
	}
	return nil, nil
}

func checkNode(pass *analysis.Pass, n ast.Node) {
	switch n := n.(type) {
	case *ast.CallExpr:
		checkUnsafeConversion(pass, n)
	case *ast.StarExpr:
		checkHeaderDeref(pass, n)
	case *ast.UnaryExpr:
		checkHeaderAddr(pass, n)
	}
}

// checkUnsafeConversion reports an unsafe.Pointer built from a uintptr the
// conversion does not derive from a pointer in the same expression.
func checkUnsafeConversion(pass *analysis.Pass, call *ast.CallExpr) {
	if !convertsType(pass.TypesInfo, call) {
		return
	}
	if !basicKind(pass.TypesInfo.TypeOf(call), types.UnsafePointer) {
		return
	}
	arg := argOf(call)
	if !basicKind(pass.TypesInfo.TypeOf(arg), types.Uintptr) {
		return
	}
	if safeUintptr(pass, arg) {
		return
	}
	pass.Reportf(call.Pos(), unsafePointerMisuse)
}

func checkHeaderDeref(pass *analysis.Pass, n *ast.StarExpr) {
	if name := reflectTypeName(pass.TypesInfo.TypeOf(n)); isHeaderName(name) {
		pass.Reportf(n.Pos(), headerMisuse, name)
	}
}

func checkHeaderAddr(pass *analysis.Pass, n *ast.UnaryExpr) {
	if n.Op != token.AND {
		return
	}
	if name := reflectTypeName(pass.TypesInfo.TypeOf(n.X)); isHeaderName(name) {
		pass.Reportf(n.Pos(), headerMisuse, name)
	}
}

func isHeaderName(name string) bool {
	return name == sliceHeader || name == stringHeader
}

func argOf(call *ast.CallExpr) ast.Expr {
	if len(call.Args) == 1 {
		return call.Args[0]
	}
	return nil
}

func convertsType(info *types.Info, call *ast.CallExpr) bool {
	return len(call.Args) == 1 && info.Types[call.Fun].IsType()
}

func basicKind(t types.Type, kind types.BasicKind) bool {
	if t == nil {
		return false
	}
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == kind
}

func safeUintptr(pass *analysis.Pass, expr ast.Expr) bool {
	switch x := ast.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		return safeHeaderData(pass, x)
	case *ast.CallExpr:
		if safeReflectCall(pass, x) {
			return true
		}
	}
	return pointerArithmetic(pass.TypesInfo, expr) == arithmeticPointer
}

// safeHeaderData reports whether sel reads the Data field of a pointer to a
// reflect header value, which keeps the original slice or string alive.
func safeHeaderData(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "Data" {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if t == nil {
		return false
	}
	ptr, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return false
	}
	return isHeaderName(reflectTypeName(ptr.Elem()))
}

// safeReflectCall reports whether call is reflect.Value.Pointer or
// reflect.Value.UnsafeAddr, which derive the uintptr from a live Value.
func safeReflectCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != reflectPkg {
		return false
	}
	if fn.Name() != "Pointer" && fn.Name() != "UnsafeAddr" {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	recv := sig.Recv()
	return recv != nil && reflectTypeName(recv.Type()) == valueType
}

type arithmeticKind uint8

const (
	arithmeticScalar arithmeticKind = iota
	arithmeticPointer
	arithmeticInvalid
)

// arithTable classifies binary pointer arithmetic: each entry maps an
// operator with left and right operand kinds to the result kind. A shape
// missing from the table is invalid, matching the loGos rule.
type arithKey struct {
	op          token.Token
	left, right arithmeticKind
}

var arithTable = map[arithKey]arithmeticKind{
	{token.ADD, arithmeticScalar, arithmeticPointer}:     arithmeticPointer,
	{token.ADD, arithmeticPointer, arithmeticScalar}:     arithmeticPointer,
	{token.SUB, arithmeticPointer, arithmeticScalar}:     arithmeticPointer,
	{token.AND_NOT, arithmeticPointer, arithmeticScalar}: arithmeticPointer,
}

func pointerArithmetic(info *types.Info, expr ast.Expr) arithmeticKind {
	switch x := ast.Unparen(expr).(type) {
	case *ast.CallExpr:
		return conversionArithmetic(info, x)
	case *ast.UnaryExpr:
		return unaryArithmetic(info, x)
	case *ast.BinaryExpr:
		return binaryArithmetic(info, x)
	}
	return arithmeticScalar
}

func unaryArithmetic(info *types.Info, expr *ast.UnaryExpr) arithmeticKind {
	if pointerArithmetic(info, expr.X) != arithmeticScalar {
		return arithmeticInvalid
	}
	return arithmeticScalar
}

func conversionArithmetic(info *types.Info, call *ast.CallExpr) arithmeticKind {
	if !convertsType(info, call) {
		return arithmeticScalar
	}
	if basicKind(info.TypeOf(call), types.Uintptr) && basicKind(info.TypeOf(call.Args[0]), types.UnsafePointer) {
		return arithmeticPointer
	}
	if pointerArithmetic(info, call.Args[0]) != arithmeticScalar {
		return arithmeticInvalid
	}
	return arithmeticScalar
}

func binaryArithmetic(info *types.Info, op *ast.BinaryExpr) arithmeticKind {
	left := pointerArithmetic(info, op.X)
	right := pointerArithmetic(info, op.Y)
	if left == arithmeticScalar && right == arithmeticScalar {
		return arithmeticScalar
	}
	if kind, ok := arithTable[arithKey{op: op.Op, left: left, right: right}]; ok {
		return kind
	}
	return arithmeticInvalid
}

func reflectTypeName(t types.Type) string {
	if t == nil {
		return ""
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != reflectPkg {
		return ""
	}
	return named.Obj().Name()
}
