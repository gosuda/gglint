// Package backingpin reports subslices that keep a large backing array alive.
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
)

const (
	limitBytes = 65536
	message    = "subslice keeps a %d-byte backing array alive: copy the bytes with slices.Clone"
)

// Analyzer reports escaping subslices of backing arrays of 64 KiB or more.
var Analyzer = &analysis.Analyzer{
	Name: "backingpin",
	Doc: "Report subslices that keep a backing array of 64 KiB or more alive.\n\n" +
		"A slice expression over an array of static size of at least 65536 bytes, or over a local slice made " +
		"once by make with a constant capacity of at least 65536 bytes, is reported when its result is " +
		"returned, stored in a field, global, map, or composite literal, or sent on a channel.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	made := madeSlices(pass, insp)
	for c := range insp.Root().Preorder((*ast.SliceExpr)(nil)) {
		s := c.Node().(*ast.SliceExpr)
		size := backingBytes(pass, made, s)
		if size >= limitBytes && escapes(pass.TypesInfo, unparenParent(c)) {
			pass.Reportf(s.Pos(), message, size)
		}
	}
	return nil, nil
}

func backingBytes(pass *analysis.Pass, made map[*types.Var]int64, s *ast.SliceExpr) int64 {
	if n := arrayBytes(pass.TypesSizes, pass.TypesInfo.TypeOf(s.X)); n > 0 {
		return n
	}
	id, ok := ast.Unparen(s.X).(*ast.Ident)
	if !ok {
		return 0
	}
	v, _ := pass.TypesInfo.Uses[id].(*types.Var)
	return made[v]
}

func arrayBytes(sizes types.Sizes, t types.Type) int64 {
	if t == nil {
		return 0
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	if _, ok := t.Underlying().(*types.Array); !ok {
		return 0
	}
	return sizes.Sizeof(t)
}

func unparenParent(c inspector.Cursor) inspector.Cursor {
	for c.ParentEdgeKind() == edge.ParenExpr_X {
		c = c.Parent()
	}
	return c
}

func escapes(info *types.Info, c inspector.Cursor) bool {
	kind, index := c.ParentEdge()
	switch kind {
	case edge.ReturnStmt_Results, edge.SendStmt_Value, edge.CompositeLit_Elts, edge.KeyValueExpr_Value:
		return true
	case edge.AssignStmt_Rhs:
		return assignsOutside(info, c.Parent().Node().(*ast.AssignStmt), index)
	case edge.ValueSpec_Values:
		return declaresGlobal(info, c.Parent().Node().(*ast.ValueSpec), index)
	}
	return false
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

type sliceTracker struct {
	info    *types.Info
	sizes   types.Sizes
	made    map[*types.Var]int64
	assigns map[*types.Var]int
}

func madeSlices(pass *analysis.Pass, insp *inspector.Inspector) map[*types.Var]int64 {
	t := sliceTracker{
		info:    pass.TypesInfo,
		sizes:   pass.TypesSizes,
		made:    map[*types.Var]int64{},
		assigns: map[*types.Var]int{},
	}
	kinds := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil), (*ast.RangeStmt)(nil)}
	for c := range insp.Root().Preorder(kinds...) {
		t.note(c.Node())
	}
	maps.DeleteFunc(t.made, func(v *types.Var, _ int64) bool { return t.assigns[v] != 1 })
	return t.made
}

func (t sliceTracker) note(n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		t.assign(n.Lhs, n.Rhs)
	case *ast.ValueSpec:
		t.declare(n)
	case *ast.RangeStmt:
		t.assign([]ast.Expr{n.Key, n.Value}, nil)
	}
}

func (t sliceTracker) declare(spec *ast.ValueSpec) {
	names := make([]ast.Expr, len(spec.Names))
	for i, name := range spec.Names {
		names[i] = name
	}
	t.assign(names, spec.Values)
}

func (t sliceTracker) assign(lhs, rhs []ast.Expr) {
	for i, target := range lhs {
		id, ok := target.(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := t.info.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		t.assigns[v]++
		if len(rhs) == len(lhs) && t.info.Defs[id] != nil {
			t.recordMake(v, rhs[i])
		}
	}
}

func (t sliceTracker) recordMake(v *types.Var, e ast.Expr) {
	if v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
		return
	}
	if n, ok := makeBytes(t.info, t.sizes, e); ok {
		t.made[v] = n
	}
}

func makeBytes(info *types.Info, sizes types.Sizes, e ast.Expr) (int64, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) < 2 || !isBuiltinMake(info, call.Fun) {
		return 0, false
	}
	slice, ok := info.TypeOf(call.Args[0]).Underlying().(*types.Slice)
	if !ok {
		return 0, false
	}
	capacity, ok := constantInt(info, call.Args[len(call.Args)-1])
	if !ok {
		return 0, false
	}
	return saturatingMul(capacity, sizes.Sizeof(slice.Elem())), true
}

func isBuiltinMake(info *types.Info, fun ast.Expr) bool {
	id, ok := ast.Unparen(fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "make"
}

func constantInt(info *types.Info, e ast.Expr) (int64, bool) {
	v := info.Types[e].Value
	if v == nil {
		return 0, false
	}
	return constant.Int64Val(constant.ToInt(v))
}

func saturatingMul(count, size int64) int64 {
	if size > 0 && count > math.MaxInt64/size {
		return math.MaxInt64
	}
	return count * size
}
