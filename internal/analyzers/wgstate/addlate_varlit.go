package wgstate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

type varAssign struct {
	lit   *ast.FuncLit
	pos   token.Pos
	inLit bool
	loops []ast.Node
}

type varCall struct {
	pos   token.Pos
	loops []ast.Node
}

type varLitSet struct {
	assigns map[*types.Var][]varAssign
	calls   map[*types.Var][]varCall
}

func newVarLitSet() *varLitSet {
	return &varLitSet{assigns: map[*types.Var][]varAssign{}, calls: map[*types.Var][]varCall{}}
}

type scopeView struct {
	loops  []ast.Node
	lits   []*ast.FuncLit
	blocks []ast.Node
}

func containsNode(outer ast.Node, pos, end token.Pos) bool {
	return outer.Pos() < pos && end <= outer.End()
}

func enclosingLoops(pos, end token.Pos, loops []ast.Node) []ast.Node {
	var out []ast.Node
	for _, l := range loops {
		if containsNode(l, pos, end) {
			out = append(out, l)
		}
	}
	return out
}

func insideLits(pos, end token.Pos, lits []*ast.FuncLit) bool {
	for _, f := range lits {
		if containsNode(f, pos, end) {
			return true
		}
	}
	return false
}

func insideSkips(pos, end token.Pos, view scopeView) bool {
	if insideLits(pos, end, view.lits) {
		return true
	}
	for _, b := range view.blocks {
		if containsNode(b, pos, end) {
			return true
		}
	}
	return false
}

func sharesLoop(a, b []ast.Node) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func canInvoke(a varAssign, c varCall) bool {
	if a.pos < c.pos {
		return true
	}
	if a.inLit {
		return true
	}
	return sharesLoop(a.loops, c.loops)
}

func scopeViewOf(body *ast.BlockStmt) scopeView {
	var view scopeView
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ForStmt:
			view.loops = append(view.loops, n)
		case *ast.RangeStmt:
			view.loops = append(view.loops, n)
		case *ast.FuncLit:
			view.lits = append(view.lits, n)
		case *ast.GoStmt:
			view.blocks = append(view.blocks, n)
		case *ast.DeferStmt:
			view.blocks = append(view.blocks, n)
		}
		return true
	})
	return view
}

func recordAssignLits(info *types.Info, body *ast.BlockStmt, set *varLitSet, view scopeView) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			noteAssignLit(info, n, set, view)
		case *ast.ValueSpec:
			noteValueLit(info, n, set, view)
		}
		return true
	})
}

func recordVarCalls(info *types.Info, body *ast.BlockStmt, set *varLitSet, view scopeView) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		case *ast.CallExpr:
			noteVarCall(info, n, set, view)
		}
		return true
	})
}

func collectVarLits(info *types.Info, body *ast.BlockStmt, set *varLitSet) {
	view := scopeViewOf(body)
	recordAssignLits(info, body, set, view)
	recordVarCalls(info, body, set, view)
}

func applyVarLits(pass *analysis.Pass, fn ast.Node, set *varLitSet, out map[flow.Key]token.Pos) {
	for v := range set.assigns {
		applyOneVarLit(pass, fn, v, set, out)
	}
}

func recordEarliest(out map[flow.Key]token.Pos, keys []flow.Key, pos token.Pos) {
	for _, k := range keys {
		if p, dup := out[k]; !dup || pos < p {
			out[k] = pos
		}
	}
}

func applyOneVarLit(pass *analysis.Pass, fn ast.Node, v *types.Var, set *varLitSet, out map[flow.Key]token.Pos) {
	if !flow.IsLocal(pass.TypesInfo, fn, v) {
		return
	}
	assigns, ok := set.assigns[v]
	if !ok {
		return
	}
	calls, ok := set.calls[v]
	if !ok {
		return
	}
	for _, a := range assigns {
		keys := litAddKeys(pass, a.lit)
		for _, c := range calls {
			if !canInvoke(a, c) {
				continue
			}
			recordEarliest(out, keys, c.pos)
		}
	}
}

func noteAssignLit(info *types.Info, s *ast.AssignStmt, set *varLitSet, view scopeView) {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}
	id, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	lit, ok := ast.Unparen(s.Rhs[0]).(*ast.FuncLit)
	if !ok {
		return
	}
	set.assigns[v] = append(set.assigns[v], varAssign{
		lit:   lit,
		pos:   s.Pos(),
		inLit: insideLits(s.Pos(), s.End(), view.lits),
		loops: enclosingLoops(s.Pos(), s.End(), view.loops),
	})
}

func noteValueLit(info *types.Info, s *ast.ValueSpec, set *varLitSet, view scopeView) {
	if len(s.Names) != 1 || len(s.Values) != 1 {
		return
	}
	v, ok := info.ObjectOf(s.Names[0]).(*types.Var)
	if !ok {
		return
	}
	lit, ok := ast.Unparen(s.Values[0]).(*ast.FuncLit)
	if !ok {
		return
	}
	set.assigns[v] = append(set.assigns[v], varAssign{
		lit:   lit,
		pos:   s.Pos(),
		inLit: insideLits(s.Pos(), s.End(), view.lits),
		loops: enclosingLoops(s.Pos(), s.End(), view.loops),
	})
}

func noteVarCall(info *types.Info, call *ast.CallExpr, set *varLitSet, view scopeView) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return
	}
	if insideSkips(call.Pos(), call.End(), view) {
		return
	}
	set.calls[v] = append(set.calls[v], varCall{
		pos:   call.Pos(),
		loops: enclosingLoops(call.Pos(), call.End(), view.loops),
	})
}
