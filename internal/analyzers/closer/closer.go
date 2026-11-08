package closer

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

var Analyzer = &analysis.Analyzer{
	Name: "closer",
	Doc: `report local closers that are never closed or handed on

A local variable declared in a function (not a parameter or named result) that
is set from a call whose result type has a Close() error method must be closed by
defer v.Close(), or by v.Close() inside a deferred function literal, or handed
on: returned, stored, sent, passed as an argument, aliased, taken as a method
value, or captured by a function literal. A method call, a field selection, a
comparison with == or !=, an assignment to the variable, and _ = v neither
close nor hand on. One finding is reported per variable, at its first
assignment from a call.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	for fn := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		s := &scan{pass: pass, fn: fn, first: map[*types.Var]*ast.Ident{}, released: map[*types.Var]bool{}}
		s.run()
	}
	return nil, nil
}

type scan struct {
	pass     *analysis.Pass
	fn       inspector.Cursor
	first    map[*types.Var]*ast.Ident
	order    []*types.Var
	released map[*types.Var]bool
}

func (s *scan) run() {
	s.collectHeld()
	if len(s.order) == 0 {
		return
	}
	s.collectReleases()
	for _, v := range s.order {
		if !s.released[v] {
			name := s.first[v].Name
			s.pass.Reportf(s.first[v].Pos(), "%s is never closed: defer %s.Close() or return it", name, name)
		}
	}
}

func (s *scan) collectHeld() {
	for cur := range s.fn.Preorder((*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil)) {
		if owner(cur) != s.fn {
			continue
		}
		switch n := cur.Node().(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE || n.Tok == token.ASSIGN {
				s.holdAll(n.Lhs, n.Rhs)
			}
		case *ast.ValueSpec:
			s.holdAll(identExprs(n.Names), n.Values)
		}
	}
}

func identExprs(ids []*ast.Ident) []ast.Expr {
	exprs := make([]ast.Expr, len(ids))
	for i, id := range ids {
		exprs[i] = id
	}
	return exprs
}

func (s *scan) holdAll(targets, values []ast.Expr) {
	switch {
	case len(values) == len(targets):
		for i, target := range targets {
			s.hold(target, values[i], s.pass.TypesInfo.TypeOf(values[i]))
		}
	case len(values) == 1:
		s.holdTuple(targets, values[0])
	}
}

func (s *scan) holdTuple(targets []ast.Expr, value ast.Expr) {
	tuple, ok := s.pass.TypesInfo.TypeOf(value).(*types.Tuple)
	if !ok || tuple.Len() != len(targets) {
		return
	}
	for i, target := range targets {
		s.hold(target, value, tuple.At(i).Type())
	}
}

func (s *scan) hold(target, value ast.Expr, result types.Type) {
	id, ok := ast.Unparen(target).(*ast.Ident)
	if !ok || !s.isCall(value) || !hasClose(result) {
		return
	}
	v, ok := s.pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !s.declaredHere(v) {
		return
	}
	if _, seen := s.first[v]; !seen {
		s.first[v] = id
		s.order = append(s.order, v)
	}
}

func (s *scan) isCall(e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fun := s.pass.TypesInfo.Types[call.Fun]
	return !fun.IsType() && !fun.IsBuiltin()
}

func (s *scan) declaredHere(v *types.Var) bool {
	fn := s.fn.Node()
	return v.Kind() == types.LocalVar && fn.Pos() <= v.Pos() && v.Pos() < fn.End()
}

func hasClose(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Close")
	method, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := method.Signature()
	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type())
}

func (s *scan) collectReleases() {
	for cur := range s.fn.Preorder((*ast.Ident)(nil), (*ast.DeferStmt)(nil)) {
		switch n := cur.Node().(type) {
		case *ast.Ident:
			s.noteUse(cur, n)
		case *ast.DeferStmt:
			s.noteDeferredClose(n.Call)
		}
	}
}

func (s *scan) noteUse(cur inspector.Cursor, id *ast.Ident) {
	v, ok := s.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return
	}
	if _, held := s.first[v]; held && (owner(cur) != s.fn || !s.isPlain(cur)) {
		s.released[v] = true
	}
}

func (s *scan) noteDeferredClose(call *ast.CallExpr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" || len(call.Args) != 0 {
		return
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := s.pass.TypesInfo.Uses[id].(*types.Var); ok {
		s.released[v] = true
	}
}

func (s *scan) isPlain(cur inspector.Cursor) bool {
	for cur.ParentEdgeKind() == edge.ParenExpr_X {
		cur = cur.Parent()
	}
	kind := cur.ParentEdgeKind()
	if kind == edge.SelectorExpr_X {
		return s.isPlainSelection(cur.Parent())
	}
	if kind == edge.BinaryExpr_X || kind == edge.BinaryExpr_Y {
		op := inspection.Node[*ast.BinaryExpr](cur.Parent()).Op
		return op == token.EQL || op == token.NEQ
	}
	if kind == edge.AssignStmt_Rhs {
		return isDiscarded(cur)
	}
	return kind == edge.AssignStmt_Lhs
}

func (s *scan) isPlainSelection(cur inspector.Cursor) bool {
	switch s.pass.TypesInfo.Selections[inspection.Node[*ast.SelectorExpr](cur)].Kind() {
	case types.FieldVal:
		return true
	case types.MethodVal:
		return cur.ParentEdgeKind() == edge.CallExpr_Fun
	case types.MethodExpr:
	}
	return false
}

func isDiscarded(cur inspector.Cursor) bool {
	assign := inspection.Node[*ast.AssignStmt](cur.Parent())
	if len(assign.Lhs) != len(assign.Rhs) {
		return false
	}
	id, ok := ast.Unparen(assign.Lhs[cur.ParentEdgeIndex()]).(*ast.Ident)
	return ok && id.Name == "_"
}

func owner(cur inspector.Cursor) inspector.Cursor {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		return fn
	}
	return inspector.Cursor{}
}
