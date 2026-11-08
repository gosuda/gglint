package backingpin

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"math"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const (
	limitBytes       = 65536
	message          = "subslice keeps a %d-byte backing array alive: copy the bytes with slices.Clone"
	substringMessage = "substring keeps a %d-byte backing string alive: copy it with strings.Clone"
)

var Analyzer = &analysis.Analyzer{
	Name: "backingpin",
	Doc: "Report subslices and substrings that keep a backing store larger than 64 KiB alive.\n\n" +
		"A slice expression is reported when its backing store is statically larger than 65536 bytes, " +
		"it does not cover the whole store, and its result is returned, stored in a field, global, map, " +
		"or composite literal, sent on a channel, or passed to a function of the package that stores or " +
		"sends that parameter. Known backing stores are arrays and pointers to arrays, make with a constant " +
		"capacity, once-assigned local slices of either, and strings converted from a byte slice of constant length.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

type checker struct {
	info   *types.Info
	sizes  types.Sizes
	values map[*types.Var]ast.Expr
	leaks  map[*types.Var]bool
}

func run(pass *analysis.Pass) (any, error) {
	insp := inspection.Of(pass)
	c := checker{
		info:   pass.TypesInfo,
		sizes:  pass.TypesSizes,
		values: staticValues(pass.TypesInfo, insp),
		leaks:  leakingParams(pass.TypesInfo, insp),
	}
	for cur := range insp.Root().Preorder((*ast.SliceExpr)(nil)) {
		s := inspection.Node[*ast.SliceExpr](cur)
		size := c.backingBytes(s)
		if size > limitBytes && !c.coversBacking(s, size) && c.escapes(unparenParent(cur)) {
			pass.Reportf(s.Pos(), c.format(s), size)
		}
	}
	return nil, nil
}

func (c checker) format(s *ast.SliceExpr) string {
	if isString(c.info.TypeOf(s.X)) {
		return substringMessage
	}
	return message
}

func (c checker) backingBytes(s *ast.SliceExpr) int64 {
	t := c.info.TypeOf(s.X)
	if isString(t) {
		return c.stringLength(s.X)
	}
	if a := arrayOf(t); a != nil {
		return c.sizes.Sizeof(a)
	}
	return c.sliceBacking(s.X)
}

func (c checker) sliceBacking(x ast.Expr) int64 {
	switch v := c.staticValue(x).(type) {
	case *ast.CallExpr:
		return c.makeBytes(v)
	case *ast.SliceExpr:
		return c.backingBytes(v)
	}
	return 0
}

func (c checker) stringLength(x ast.Expr) int64 {
	call, ok := c.staticValue(x).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !c.info.Types[call.Fun].IsType() || !isByteSlice(c.info.TypeOf(call.Args[0])) {
		return 0
	}
	return c.sliceLength(call.Args[0])
}

func (c checker) sliceLength(x ast.Expr) int64 {
	switch v := c.staticValue(x).(type) {
	case *ast.CallExpr:
		return c.makeLength(v)
	case *ast.SliceExpr:
		if v.Max == nil {
			return c.slicedLength(v)
		}
	}
	return 0
}

func (c checker) slicedLength(s *ast.SliceExpr) int64 {
	lo, ok := c.boundOr(s.Low, 0)
	if !ok {
		return 0
	}
	hi, ok := c.boundOr(s.High, c.operandLength(s))
	if !ok || hi < lo {
		return 0
	}
	return hi - lo
}

func (c checker) operandLength(s *ast.SliceExpr) int64 {
	if a := arrayOf(c.info.TypeOf(s.X)); a != nil {
		return a.Len()
	}
	return c.sliceLength(s.X)
}

func (c checker) coversBacking(s *ast.SliceExpr, size int64) bool {
	if s.Max != nil {
		return false
	}
	if lo, ok := c.boundOr(s.Low, 0); !ok || lo != 0 {
		return false
	}
	if s.High == nil {
		return true
	}
	if hi, ok := c.constInt(s.High); ok {
		elem := c.elemSize(s)
		return elem > 0 && hi == size/elem
	}
	return c.isLenOf(s.High, s.X)
}

func (c checker) elemSize(s *ast.SliceExpr) int64 {
	slice, ok := c.info.TypeOf(s).Underlying().(*types.Slice)
	if !ok {
		return 1
	}
	return c.sizes.Sizeof(slice.Elem())
}

func (c checker) isLenOf(e, x ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	return ok && len(call.Args) == 1 && isBuiltin(c.info, call.Fun, "len") && c.sameSafe(call.Args[0], x)
}

func (c checker) sameSafe(a, b ast.Expr) bool {
	switch a := ast.Unparen(a).(type) {
	case *ast.Ident:
		b, ok := ast.Unparen(b).(*ast.Ident)
		return ok && c.info.ObjectOf(a) != nil && c.info.ObjectOf(a) == c.info.ObjectOf(b)
	case *ast.SelectorExpr:
		b, ok := ast.Unparen(b).(*ast.SelectorExpr)
		return ok && c.info.ObjectOf(a.Sel) == c.info.ObjectOf(b.Sel) && c.sameSafe(a.X, b.X)
	}
	return false
}

func (c checker) boundOr(bound ast.Expr, def int64) (int64, bool) {
	if bound == nil {
		return def, true
	}
	return c.constInt(bound)
}

func (c checker) constInt(e ast.Expr) (int64, bool) {
	v := c.info.Types[c.staticValue(e)].Value
	if v == nil {
		return 0, false
	}
	return constant.Int64Val(constant.ToInt(v))
}

func (c checker) staticValue(e ast.Expr) ast.Expr {
	e = ast.Unparen(e)
	id, ok := e.(*ast.Ident)
	if !ok {
		return e
	}
	v, ok := c.info.Uses[id].(*types.Var)
	if !ok {
		return e
	}
	value, ok := c.values[v]
	if !ok {
		return e
	}
	return c.staticValue(value)
}

func (c checker) makeCall(call *ast.CallExpr) *types.Slice {
	if len(call.Args) < 2 || !isBuiltin(c.info, call.Fun, "make") {
		return nil
	}
	slice, ok := c.info.TypeOf(call.Args[0]).Underlying().(*types.Slice)
	if !ok {
		return nil
	}
	return slice
}

func (c checker) makeBytes(call *ast.CallExpr) int64 {
	slice := c.makeCall(call)
	if slice == nil {
		return 0
	}
	capacity, ok := c.constInt(call.Args[len(call.Args)-1])
	if !ok {
		return 0
	}
	return saturatingMul(capacity, c.sizes.Sizeof(slice.Elem()))
}

func (c checker) makeLength(call *ast.CallExpr) int64 {
	if c.makeCall(call) == nil {
		return 0
	}
	length, _ := c.constInt(call.Args[1])
	return length
}

func (c checker) escapes(cur inspector.Cursor) bool {
	if kind, index := cur.ParentEdge(); kind == edge.CallExpr_Args {
		return c.leaksArg(inspection.Node[*ast.CallExpr](cur.Parent()), index)
	}
	return escapesDirectly(c.info, cur)
}

func (c checker) leaksArg(call *ast.CallExpr, index int) bool {
	fn := typeutil.StaticCallee(c.info, call)
	if fn == nil {
		return false
	}
	sig := fn.Origin().Signature()
	params := sig.Params()
	if index >= params.Len() || sig.Variadic() && index >= params.Len()-1 {
		return false
	}
	return c.leaks[params.At(index)]
}

func leakingParams(info *types.Info, insp *inspector.Inspector) map[*types.Var]bool {
	leaks := map[*types.Var]bool{}
	for cur := range insp.Root().Preorder((*ast.Ident)(nil)) {
		v, ok := info.Uses[inspection.Node[*ast.Ident](cur)].(*types.Var)
		parent := unparenParent(cur)
		if ok && v.Kind() == types.ParamVar && parent.ParentEdgeKind() != edge.ReturnStmt_Results &&
			escapesDirectly(info, parent) {
			leaks[v] = true
		}
	}
	return leaks
}

func arrayOf(t types.Type) *types.Array {
	if t == nil {
		return nil
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	if a, ok := t.Underlying().(*types.Array); ok {
		return a
	}
	return nil
}

func isString(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isByteSlice(t types.Type) bool {
	if t == nil {
		return false
	}
	slice, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	b, ok := slice.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Byte
}

func unparenParent(c inspector.Cursor) inspector.Cursor {
	for c.ParentEdgeKind() == edge.ParenExpr_X {
		c = c.Parent()
	}
	return c
}

func escapesDirectly(info *types.Info, c inspector.Cursor) bool {
	kind, index := c.ParentEdge()
	if kind == edge.AssignStmt_Rhs {
		return assignsOutside(info, inspection.Node[*ast.AssignStmt](c.Parent()), index)
	}
	if kind == edge.ValueSpec_Values {
		return declaresGlobal(info, inspection.Node[*ast.ValueSpec](c.Parent()), index)
	}
	return kind == edge.ReturnStmt_Results || kind == edge.SendStmt_Value ||
		kind == edge.CompositeLit_Elts || kind == edge.KeyValueExpr_Value
}

func assignsOutside(info *types.Info, as *ast.AssignStmt, index int) bool {
	return as.Tok == token.ASSIGN && len(as.Lhs) == len(as.Rhs) && storesOutside(info, as.Lhs[index])
}

func declaresGlobal(info *types.Info, spec *ast.ValueSpec, index int) bool {
	return index < len(spec.Names) && isPackageVar(info.Defs[spec.Names[index]])
}

func storesOutside(info *types.Info, lhs ast.Expr) bool {
	switch x := ast.Unparen(lhs).(type) {
	case *ast.Ident:
		return isPackageVar(info.Uses[x])
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil {
			return sel.Kind() == types.FieldVal
		}
		return isPackageVar(info.Uses[x.Sel])
	case *ast.IndexExpr:
		return isMap(info.TypeOf(x.X))
	}
	return false
}

func isPackageVar(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	return ok && !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

func isMap(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

type valueTracker struct {
	info    *types.Info
	values  map[*types.Var]ast.Expr
	assigns map[*types.Var]int
}

func staticValues(info *types.Info, insp *inspector.Inspector) map[*types.Var]ast.Expr {
	t := valueTracker{
		info:    info,
		values:  map[*types.Var]ast.Expr{},
		assigns: map[*types.Var]int{},
	}
	kinds := []ast.Node{
		(*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil), (*ast.RangeStmt)(nil),
		(*ast.IncDecStmt)(nil), (*ast.UnaryExpr)(nil),
	}
	for c := range insp.Root().Preorder(kinds...) {
		t.note(c.Node())
	}
	maps.DeleteFunc(t.values, func(v *types.Var, _ ast.Expr) bool { return t.assigns[v] != 1 })
	return t.values
}

func (t valueTracker) note(n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		t.assign(n.Lhs, n.Rhs)
	case *ast.ValueSpec:
		t.declare(n)
	case *ast.RangeStmt:
		t.assign([]ast.Expr{n.Key, n.Value}, nil)
	case *ast.IncDecStmt:
		t.assign([]ast.Expr{n.X}, nil)
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			t.assign([]ast.Expr{n.X}, nil)
		}
	}
}

func (t valueTracker) declare(spec *ast.ValueSpec) {
	names := make([]ast.Expr, len(spec.Names))
	for i, name := range spec.Names {
		names[i] = name
	}
	t.assign(names, spec.Values)
}

func (t valueTracker) assign(lhs, rhs []ast.Expr) {
	for i, target := range lhs {
		id, ok := ast.Unparen(target).(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := t.info.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		t.assigns[v]++
		if len(rhs) == len(lhs) && t.info.Defs[id] != nil && isLocal(v) {
			t.values[v] = rhs[i]
		}
	}
}

func isLocal(v *types.Var) bool {
	return v.Pkg() != nil && v.Parent() != v.Pkg().Scope()
}

func isBuiltin(info *types.Info, fun ast.Expr, name string) bool {
	id, ok := ast.Unparen(fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
}

func saturatingMul(count, size int64) int64 {
	if size > 0 && count > math.MaxInt64/size {
		return math.MaxInt64
	}
	return count * size
}
