package stackaddr

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"iter"

	"golang.org/x/tools/go/analysis"
)

const (
	integerMessage = "%s keeps the address of stack storage as an integer the runtime cannot track: keep an unsafe.Pointer"
	pointerMessage = "%s takes the address of stack storage and stores it outside its frame"
)

var Analyzer = &analysis.Analyzer{
	Name: "stackaddr",
	Doc: `report stack addresses kept past their frame through unsafe

Stack storage is a local variable or parameter, a field of a local struct
reached through no pointer, an element of a local array, and the result of new
or of &T{...}. A local that every assignment sets to such an address, or to
unsafe.Pointer of one, stands for it as well. A slice element, a map entry, and
anything reached through a pointer is not stack storage.

The integer form uintptr(unsafe.Pointer(&x)), and arithmetic on it, is reported
wherever it is stored, returned, or sent: an integer keeps nothing alive and does
not follow the object when the stack moves. The pointer form unsafe.Pointer(&x)
is reported only when it leaves the frame: returned, sent, assigned to a named
result, or stored outside stack storage. unsafe.Pointer(uintptr(...) + off)
turns an integer form back into a pointer form. A call argument, a comparison,
and an assignment to the blank identifier store nothing.`,
	Run: run,
}

type addrKind int

const (
	addrNone addrKind = iota
	addrInteger
	addrPointer
)

type pointerState int

const (
	pointerUnset pointerState = iota
	pointerFrame
	pointerOther
)

type frameScan struct {
	pass     *analysis.Pass
	results  map[*types.Var]bool
	pointers map[*types.Var]pointerState
	addrs    map[*types.Var]*ast.CallExpr
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if importsUnsafe(file) {
			ast.Inspect(file, func(n ast.Node) bool { return scanFunctions(pass, n) })
		}
	}
	return nil, nil
}

func importsUnsafe(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path.Value == `"unsafe"` {
			return true
		}
	}
	return false
}

func scanFunctions(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.FuncDecl:
		if n.Body != nil {
			newFrameScan(pass).run(n)
		}
		return false
	case *ast.FuncLit:
		newFrameScan(pass).run(n)
		return false
	}
	return true
}

func newFrameScan(pass *analysis.Pass) *frameScan {
	return &frameScan{
		pass:     pass,
		results:  make(map[*types.Var]bool),
		pointers: make(map[*types.Var]pointerState),
		addrs:    make(map[*types.Var]*ast.CallExpr),
	}
}

func (s *frameScan) run(root ast.Node) {
	ast.Inspect(root, s.collect)
	ast.Inspect(root, s.scan)
}

func (s *frameScan) collect(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.FuncType:
		s.noteSignature(n)
	case *ast.AssignStmt:
		s.noteAssign(n)
	case *ast.ValueSpec:
		s.noteDecl(n)
	case *ast.RangeStmt:
		s.noteTarget(n.Key, nil)
		s.noteTarget(n.Value, nil)
	}
	return true
}

func (s *frameScan) noteSignature(ft *ast.FuncType) {
	for v := range s.fieldVars(ft.Params) {
		s.pointers[v] = pointerOther
	}
	for v := range s.fieldVars(ft.Results) {
		s.results[v] = true
	}
}

func (s *frameScan) fieldVars(list *ast.FieldList) iter.Seq[*types.Var] {
	return func(yield func(*types.Var) bool) {
		if list == nil {
			return
		}
		for _, field := range list.List {
			for _, name := range field.Names {
				if v := s.localVar(name); v != nil && !yield(v) {
					return
				}
			}
		}
	}
}

func (s *frameScan) noteAssign(a *ast.AssignStmt) {
	values := a.Rhs
	if a.Tok != token.ASSIGN && a.Tok != token.DEFINE {
		values = nil
	}
	for i, target := range a.Lhs {
		s.noteTarget(target, pairedValue(values, len(a.Lhs), i))
	}
}

func (s *frameScan) noteDecl(spec *ast.ValueSpec) {
	if spec.Values == nil {
		return
	}
	for i, name := range spec.Names {
		s.noteTarget(name, pairedValue(spec.Values, len(spec.Names), i))
	}
}

func pairedValue(values []ast.Expr, targets, i int) ast.Expr {
	if len(values) != targets {
		return nil
	}
	return values[i]
}

func (s *frameScan) noteTarget(target, value ast.Expr) {
	id, ok := ast.Unparen(target).(*ast.Ident)
	if !ok {
		return
	}
	v := s.localVar(id)
	if v == nil {
		return
	}
	delete(s.addrs, v)
	if s.pointers[v] == pointerOther {
		return
	}
	kind, call := s.carry(value)
	switch {
	case kind == addrPointer:
		s.pointers[v] = pointerFrame
		s.addrs[v] = call
	case s.directAddr(value):
		s.pointers[v] = pointerFrame
	default:
		s.pointers[v] = pointerOther
	}
}

func (s *frameScan) scan(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		s.assign(n)
	case *ast.ValueSpec:
		s.declare(n)
	case *ast.ReturnStmt:
		for _, result := range n.Results {
			s.leave(result)
		}
	case *ast.SendStmt:
		s.leave(n.Value)
	}
	return true
}

func (s *frameScan) assign(a *ast.AssignStmt) {
	if len(a.Lhs) != len(a.Rhs) {
		return
	}
	for i, target := range a.Lhs {
		s.store(target, a.Rhs[i])
	}
}

func (s *frameScan) declare(spec *ast.ValueSpec) {
	if len(spec.Values) != len(spec.Names) {
		return
	}
	for i, name := range spec.Names {
		s.store(name, spec.Values[i])
	}
}

func (s *frameScan) store(target, value ast.Expr) {
	if isBlank(target) {
		return
	}
	if s.withinFrame(target) {
		s.keep(value)
		return
	}
	s.leave(value)
}

func isBlank(x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	return ok && id.Name == "_"
}

func (s *frameScan) withinFrame(target ast.Expr) bool {
	v := s.frameRoot(target)
	return v != nil && !s.results[v]
}

func (s *frameScan) leave(x ast.Expr) {
	if kind, call := s.carry(x); kind != addrNone {
		s.report(kind, call)
	}
}

func (s *frameScan) keep(x ast.Expr) {
	if kind, call := s.carry(x); kind == addrInteger {
		s.report(kind, call)
	}
}

func (s *frameScan) report(kind addrKind, call *ast.CallExpr) {
	format := integerMessage
	if kind == addrPointer {
		format = pointerMessage
	}
	s.pass.ReportRangef(call, format, types.ExprString(call))
}

func (s *frameScan) carry(x ast.Expr) (addrKind, *ast.CallExpr) {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		return s.carryVar(x)
	case *ast.CallExpr:
		return s.carryConversion(x)
	case *ast.BinaryExpr:
		return s.carryArith(x)
	case *ast.CompositeLit:
		return s.carryElems(x)
	case *ast.UnaryExpr:
		if lit, ok := ast.Unparen(x.X).(*ast.CompositeLit); x.Op == token.AND && ok {
			return s.carryElems(lit)
		}
	}
	return addrNone, nil
}

func (s *frameScan) carryVar(id *ast.Ident) (addrKind, *ast.CallExpr) {
	if call := s.addrs[s.localVar(id)]; call != nil {
		return addrPointer, call
	}
	return addrNone, nil
}

func (s *frameScan) carryConversion(c *ast.CallExpr) (addrKind, *ast.CallExpr) {
	switch {
	case isConversionTo(s.pass.TypesInfo, c, types.UnsafePointer):
		if kind, _ := s.carry(c.Args[0]); kind != addrNone || s.frameAddr(c.Args[0]) {
			return addrPointer, c
		}
	case isConversionTo(s.pass.TypesInfo, c, types.Uintptr):
		if kind, _ := s.carry(c.Args[0]); kind != addrNone {
			return addrInteger, c
		}
	}
	return addrNone, nil
}

func (s *frameScan) carryArith(op *ast.BinaryExpr) (addrKind, *ast.CallExpr) {
	if op.Op.Precedence() <= token.EQL.Precedence() {
		return addrNone, nil
	}
	if kind, call := s.carry(op.X); kind != addrNone {
		return kind, call
	}
	return s.carry(op.Y)
}

func (s *frameScan) carryElems(lit *ast.CompositeLit) (addrKind, *ast.CallExpr) {
	found, at := addrNone, (*ast.CallExpr)(nil)
	for _, x := range elemExprs(lit) {
		kind, call := s.carry(x)
		if kind == addrInteger {
			return kind, call
		}
		if found == addrNone {
			found, at = kind, call
		}
	}
	return found, at
}

func elemExprs(lit *ast.CompositeLit) []ast.Expr {
	exprs := make([]ast.Expr, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			exprs = append(exprs, kv.Key, kv.Value)
			continue
		}
		exprs = append(exprs, elt)
	}
	return exprs
}

func (s *frameScan) frameAddr(x ast.Expr) bool {
	if s.directAddr(x) {
		return true
	}
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return false
	}
	v := s.localVar(id)
	return v != nil && s.pointers[v] == pointerFrame
}

func (s *frameScan) directAddr(x ast.Expr) bool {
	switch x := ast.Unparen(x).(type) {
	case *ast.UnaryExpr:
		return x.Op == token.AND && s.frameObject(x.X)
	case *ast.CallExpr:
		return s.callsNew(x)
	}
	return false
}

func (s *frameScan) frameObject(x ast.Expr) bool {
	if _, ok := ast.Unparen(x).(*ast.CompositeLit); ok {
		return true
	}
	return s.frameRoot(x) != nil
}

func (s *frameScan) callsNew(call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "new" {
		return false
	}
	_, ok = s.pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok
}

func (s *frameScan) frameRoot(x ast.Expr) *types.Var {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		return s.localVar(x)
	case *ast.SelectorExpr:
		if sel := s.pass.TypesInfo.Selections[x]; sel != nil && sel.Kind() == types.FieldVal && !sel.Indirect() {
			return s.frameRoot(x.X)
		}
	case *ast.IndexExpr:
		if _, ok := s.pass.TypesInfo.TypeOf(x.X).Underlying().(*types.Array); ok {
			return s.frameRoot(x.X)
		}
	}
	return nil
}

func (s *frameScan) localVar(id *ast.Ident) *types.Var {
	v, ok := cmp.Or(s.pass.TypesInfo.Defs[id], s.pass.TypesInfo.Uses[id]).(*types.Var)
	if !ok || v.Kind() == types.PackageVar || v.Kind() == types.FieldVar {
		return nil
	}
	return v
}

func isConversionTo(info *types.Info, call *ast.CallExpr, kind types.BasicKind) bool {
	if len(call.Args) != 1 || !info.Types[call.Fun].IsType() {
		return false
	}
	basic, ok := info.TypeOf(call).Underlying().(*types.Basic)
	return ok && basic.Kind() == kind
}
