package backingpin

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"math"
	"slices"
	"strconv"

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
		"it does not cover the whole store, and its value outlives the function: it is returned, sent on " +
		"a channel, stored in a package variable, in a field or element reached through a pointer, or in a " +
		"map or slice not made in the function, or passed to a function of the package that does one of " +
		"these with that parameter. A local variable, a field of a local value, an element of a local map " +
		"or slice, and a composite literal escape only when they do. Known backing stores are arrays and " +
		"pointers to arrays, make with a constant capacity, once-assigned local slices of either, and " +
		"strings converted from a byte slice of constant length; a store whose type depends on a type " +
		"parameter has no static size.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

type flow int

const (
	stays flow = iota
	returned
	outlives
)

type checker struct {
	info   *types.Info
	sizes  types.Sizes
	values map[*types.Var]ast.Expr
	uses   map[*types.Var][]inspector.Cursor
	flows  map[varPart]flow
}

type part struct {
	field types.Object
	elem  bool
	index string
}

type varPart struct {
	v    *types.Var
	part part
}

func run(pass *analysis.Pass) (any, error) {
	insp := inspection.Of(pass)
	c := checker{
		info:   pass.TypesInfo,
		sizes:  pass.TypesSizes,
		values: staticValues(pass.TypesInfo, insp),
		uses:   varUses(pass.TypesInfo, insp),
		flows:  map[varPart]flow{},
	}
	for cur := range insp.Root().Preorder((*ast.SliceExpr)(nil)) {
		s := inspection.As[*ast.SliceExpr](cur.Node())
		size := c.backingBytes(s)
		if size > limitBytes && !c.coversBacking(s, size) && c.flowOf(cur) != stays {
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
		return c.sizeOf(a)
	}
	return c.sliceBacking(s.X)
}

func (c checker) sizeOf(t types.Type) int64 {
	if hasTypeParam(t) {
		return 0
	}
	return c.sizes.Sizeof(t)
}

func hasTypeParam(t types.Type) bool {
	switch t := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		return hasTypeParam(t.Underlying())
	case *types.Array:
		return hasTypeParam(t.Elem())
	case *types.Struct:
		for field := range t.Fields() {
			if hasTypeParam(field.Type()) {
				return true
			}
		}
	}
	return false
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
	length, _ := c.sliceLength(call.Args[0])
	return length
}

func (c checker) sliceLength(x ast.Expr) (int64, bool) {
	switch v := c.staticValue(x).(type) {
	case *ast.CallExpr:
		return c.makeLength(v)
	case *ast.SliceExpr:
		if v.Max == nil {
			return c.slicedLength(v)
		}
	}
	return 0, false
}

func (c checker) slicedLength(s *ast.SliceExpr) (int64, bool) {
	lo, ok := c.boundOr(s.Low, 0)
	if !ok {
		return 0, false
	}
	hi, ok := c.operandLength(s)
	if s.High != nil {
		hi, ok = c.constInt(s.High)
	}
	if !ok || hi < lo {
		return 0, false
	}
	return hi - lo, true
}

func (c checker) operandLength(s *ast.SliceExpr) (int64, bool) {
	if a := arrayOf(c.info.TypeOf(s.X)); a != nil {
		return a.Len(), true
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
	var hi int64
	var ok bool
	if s.High == nil || c.isLenOf(s.High, s.X) {
		hi, ok = c.operandLength(s)
		if !ok {
			return true
		}
	} else if hi, ok = c.constInt(s.High); !ok {
		return false
	}
	elem := c.elemSize(s)
	return elem > 0 && hi == size/elem
}

func (c checker) elemSize(s *ast.SliceExpr) int64 {
	slice, ok := c.info.TypeOf(s).Underlying().(*types.Slice)
	if !ok {
		return 1
	}
	return c.sizeOf(slice.Elem())
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
	if slice, ok := c.info.TypeOf(call.Args[0]).Underlying().(*types.Slice); ok {
		return slice
	}
	return nil
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
	return saturatingMul(capacity, c.sizeOf(slice.Elem()))
}

func (c checker) makeLength(call *ast.CallExpr) (int64, bool) {
	if c.makeCall(call) == nil {
		return 0, false
	}
	return c.constInt(call.Args[1])
}

func (c checker) flowOf(cur inspector.Cursor) flow {
	cur = unparenParent(cur)
	kind, index := cur.ParentEdge()
	switch kind { //nolint:exhaustive // every other edge keeps the value inside the function
	case edge.ReturnStmt_Results:
		return returned
	case edge.SendStmt_Value:
		return outlives
	case edge.AssignStmt_Rhs:
		return c.assignFlow(inspection.As[*ast.AssignStmt](cur.Parent().Node()), index)
	case edge.ValueSpec_Values:
		return c.specFlow(inspection.As[*ast.ValueSpec](cur.Parent().Node()), index)
	case edge.CompositeLit_Elts, edge.KeyValueExpr_Value:
		return c.literalFlow(cur)
	case edge.KeyValueExpr_Key:
		return c.flowOf(cur.Parent().Parent())
	case edge.UnaryExpr_X:
		return c.addressFlow(cur.Parent())
	case edge.CallExpr_Args:
		return c.argFlow(cur.Parent(), index)
	}
	return stays
}

func (c checker) addressFlow(cur inspector.Cursor) flow {
	if inspection.As[*ast.UnaryExpr](cur.Node()).Op != token.AND {
		return stays
	}
	return c.flowOf(cur)
}

func (c checker) assignFlow(as *ast.AssignStmt, index int) flow {
	if len(as.Lhs) != len(as.Rhs) || as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
		return stays
	}
	return c.storeFlow(as.Lhs[index])
}

func (c checker) specFlow(spec *ast.ValueSpec, index int) flow {
	return c.varFlow(c.specVar(spec, index))
}

func (c checker) argFlow(cur inspector.Cursor, index int) flow {
	call := inspection.As[*ast.CallExpr](cur.Node())
	fn := typeutil.StaticCallee(c.info, call)
	if fn == nil || c.isMethodExpr(call.Fun) {
		return stays
	}
	sig := fn.Origin().Signature()
	params := sig.Params()
	if index >= params.Len() || sig.Variadic() && index >= params.Len()-1 {
		return stays
	}
	f := c.varFlow(params.At(index))
	if f == returned {
		return c.flowOf(cur)
	}
	return f
}

func (c checker) isMethodExpr(fun ast.Expr) bool {
	sel, ok := ast.Unparen(fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	selection := c.info.Selections[sel]
	return selection != nil && selection.Kind() == types.MethodExpr
}

func (c checker) storeFlow(lhs ast.Expr) flow {
	switch x := ast.Unparen(lhs).(type) {
	case *ast.Ident:
		return c.varFlow(c.info.ObjectOf(x))
	case *ast.SelectorExpr:
		return c.fieldFlow(x)
	case *ast.IndexExpr:
		return c.elemFlow(x)
	case *ast.StarExpr:
		return outlives
	}
	return stays
}

func (c checker) fieldFlow(x *ast.SelectorExpr) flow {
	sel := c.info.Selections[x]
	if sel == nil {
		return c.varFlow(c.info.Uses[x.Sel])
	}
	if sel.Kind() != types.FieldVal {
		return stays
	}
	if sel.Indirect() {
		return outlives
	}
	if id, ok := ast.Unparen(x.X).(*ast.Ident); ok {
		return c.trackedFlow(c.info.ObjectOf(id), part{field: sel.Obj()})
	}
	return c.storeFlow(x.X)
}

func (c checker) elemFlow(x *ast.IndexExpr) flow {
	p := part{elem: true, index: c.constText(x.Index)}
	switch c.info.TypeOf(x.X).Underlying().(type) {
	case *types.Array:
		return c.arrayElemFlow(x.X, p)
	case *types.Map, *types.Slice:
		return c.containerFlow(x.X, p)
	}
	return outlives
}

func (c checker) arrayElemFlow(e ast.Expr, p part) flow {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		return c.trackedFlow(c.info.ObjectOf(id), p)
	}
	return c.storeFlow(e)
}

func (c checker) containerFlow(e ast.Expr, p part) flow {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return outlives
	}
	v, ok := c.info.Uses[id].(*types.Var)
	if !ok || !c.isFresh(c.values[v]) {
		return outlives
	}
	return c.trackedFlow(v, p)
}

func (c checker) literalFlow(elem inspector.Cursor) flow {
	lit, p := c.literalPart(elem)
	if v := c.boundVar(lit); v != nil {
		return c.trackedFlow(v, p)
	}
	return c.flowOf(lit)
}

func (c checker) literalPart(elem inspector.Cursor) (inspector.Cursor, part) {
	if elem.ParentEdgeKind() == edge.KeyValueExpr_Value {
		kv := inspection.As[*ast.KeyValueExpr](elem.Parent().Node())
		lit := elem.Parent().Parent()
		return lit, c.keyedPart(inspection.As[*ast.CompositeLit](lit.Node()), kv.Key)
	}
	_, pos := elem.ParentEdge()
	lit := elem.Parent()
	return lit, c.positionalPart(inspection.As[*ast.CompositeLit](lit.Node()), pos)
}

func (c checker) keyedPart(lit *ast.CompositeLit, key ast.Expr) part {
	if _, ok := c.info.TypeOf(lit).Underlying().(*types.Struct); ok {
		if id, ok := key.(*ast.Ident); ok {
			return part{field: c.info.ObjectOf(id)}
		}
	}
	return part{elem: true, index: c.constText(key)}
}

func (c checker) positionalPart(lit *ast.CompositeLit, pos int) part {
	if st, ok := c.info.TypeOf(lit).Underlying().(*types.Struct); ok {
		return part{field: st.Field(pos)}
	}
	if slices.ContainsFunc(lit.Elts, isKeyValue) {
		return part{elem: true}
	}
	return part{elem: true, index: strconv.Itoa(pos)}
}

func isKeyValue(e ast.Expr) bool {
	_, ok := e.(*ast.KeyValueExpr)
	return ok
}

func (c checker) boundVar(lit inspector.Cursor) types.Object {
	lit = unparenParent(lit)
	kind, index := lit.ParentEdge()
	if kind == edge.ValueSpec_Values {
		return c.specVar(inspection.As[*ast.ValueSpec](lit.Parent().Node()), index)
	}
	if kind == edge.AssignStmt_Rhs {
		return c.assignedVar(inspection.As[*ast.AssignStmt](lit.Parent().Node()), index)
	}
	return nil
}

func (c checker) specVar(spec *ast.ValueSpec, index int) types.Object {
	if len(spec.Names) != len(spec.Values) {
		return nil
	}
	return c.info.Defs[spec.Names[index]]
}

func (c checker) assignedVar(as *ast.AssignStmt, index int) types.Object {
	if len(as.Lhs) != len(as.Rhs) || as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
		return nil
	}
	if id, ok := ast.Unparen(as.Lhs[index]).(*ast.Ident); ok {
		return c.info.ObjectOf(id)
	}
	return nil
}

func (c checker) constText(e ast.Expr) string {
	if v := c.info.Types[e].Value; v != nil {
		return v.ExactString()
	}
	return ""
}

func (c checker) isFresh(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.CompositeLit:
		return true
	case *ast.CallExpr:
		return isBuiltin(c.info, e.Fun, "make")
	}
	return false
}

func (c checker) varFlow(obj types.Object) flow {
	return c.trackedFlow(obj, part{})
}

func (c checker) trackedFlow(obj types.Object, p part) flow {
	v, ok := obj.(*types.Var)
	if !ok {
		return stays
	}
	if isPackageVar(v) {
		return outlives
	}
	key := varPart{v: v, part: p}
	if f, done := c.flows[key]; done {
		return f
	}
	c.flows[key] = stays
	f := stays
	for _, use := range c.uses[v] {
		f = max(f, c.useFlow(unparenParent(use), p))
	}
	c.flows[key] = f
	return f
}

func (c checker) useFlow(use inspector.Cursor, p part) flow {
	kind := use.ParentEdgeKind()
	if p.elem && kind == edge.IndexExpr_X {
		return c.elemReadFlow(use.Parent(), p.index)
	}
	if p.field != nil && kind == edge.SelectorExpr_X {
		return c.fieldReadFlow(use.Parent(), p.field)
	}
	return c.flowOf(use)
}

func (c checker) elemReadFlow(read inspector.Cursor, stored string) flow {
	got := c.constText(inspection.As[*ast.IndexExpr](read.Node()).Index)
	if stored != "" && got != "" && got != stored {
		return stays
	}
	return c.flowOf(read)
}

func (c checker) fieldReadFlow(read inspector.Cursor, field types.Object) flow {
	sel := c.info.Selections[inspection.As[*ast.SelectorExpr](read.Node())]
	if sel == nil || sel.Obj() != field {
		return stays
	}
	return c.flowOf(read)
}

func varUses(info *types.Info, insp *inspector.Inspector) map[*types.Var][]inspector.Cursor {
	uses := map[*types.Var][]inspector.Cursor{}
	for cur := range insp.Root().Preorder((*ast.Ident)(nil)) {
		if v, ok := info.Uses[inspection.As[*ast.Ident](cur.Node())].(*types.Var); ok {
			uses[v] = append(uses[v], cur)
		}
	}
	return uses
}

func arrayOf(t types.Type) *types.Array {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	if a, ok := t.Underlying().(*types.Array); ok {
		return a
	}
	return nil
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isByteSlice(t types.Type) bool {
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

func isPackageVar(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	return ok && !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
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
		if len(rhs) == len(lhs) && t.info.Defs[id] != nil && !isPackageVar(v) {
			t.values[v] = rhs[i]
		}
	}
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
