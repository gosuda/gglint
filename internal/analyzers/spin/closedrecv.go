package spin

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var closedrecvAnalyzer = &analysis.Analyzer{
	Name:     "closedrecv",
	Doc:      "report single-value receives spinning after close; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runClosedrecv,
}

func runClosedrecv(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkClosedrecv(pass, cur, loop)
	})
	return nil, nil
}

func checkClosedrecv(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) {
	if !bareLoop(loop) {
		return
	}
	recv := loneRecv(pass.TypesInfo, loop.Body)
	if recv == nil {
		return
	}
	v, ok := recvVar(pass, recv)
	if !ok {
		return
	}
	fn := enclosingFunc(cur)
	if !isLocalVar(fn, v) || !isChan(v.Type()) {
		return
	}
	if !closesBefore(pass, fn, loop, v) {
		return
	}
	pass.Reportf(loop.Pos(), "after close, this receive from %s returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false", v.Name())
}

func bareLoop(loop *ast.ForStmt) bool {
	return loop.Cond == nil && loop.Body != nil
}

func recvVar(pass *analysis.Pass, recv *ast.UnaryExpr) (*types.Var, bool) {
	id, ok := ast.Unparen(recv.X).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, false
	}
	return v, true
}

func loneRecv(info *types.Info, body *ast.BlockStmt) *ast.UnaryExpr {
	s := &loneScan{clean: true, info: info}
	walkNoLit(body, s.visit)
	if !s.clean || s.ops != 1 || s.two {
		return nil
	}
	return s.recv
}

type loneScan struct {
	recv  *ast.UnaryExpr
	ops   int
	clean bool
	two   bool
	info  *types.Info
}

func (s *loneScan) visit(n ast.Node) bool {
	switch t := n.(type) {
	case *ast.CallExpr:
		s.markCall(t)
	case *ast.SelectStmt:
		s.clean = false
	case *ast.SendStmt:
		s.ops++
	case *ast.UnaryExpr:
		s.addRecv(t)
	case *ast.AssignStmt:
		s.addAssign(t)
	case *ast.ValueSpec:
		s.addValue(t)
	}
	return s.clean && s.ops < 2 && !s.two
}

func (s *loneScan) markCall(t *ast.CallExpr) {
	if isCodeCall(s.info, t) {
		s.clean = false
	}
}

func (s *loneScan) addRecv(t *ast.UnaryExpr) {
	if t.Op != token.ARROW {
		return
	}
	s.ops++
	s.recv = t
}

func (s *loneScan) addAssign(t *ast.AssignStmt) {
	if twoExprs(t.Lhs, t.Rhs) {
		s.two = true
	}
}

func (s *loneScan) addValue(t *ast.ValueSpec) {
	if len(t.Names) == 2 && len(t.Values) == 1 && hasRecv(t.Values[0]) {
		s.two = true
	}
}

func twoExprs(lhs, rhs []ast.Expr) bool {
	if len(lhs) != 2 || len(rhs) != 1 {
		return false
	}
	return hasRecv(rhs[0])
}

func hasRecv(e ast.Expr) bool {
	found := false
	walkNoLit(e, func(n ast.Node) bool {
		if recv, ok := n.(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
			found = true
		}
		return !found
	})
	return found
}

func isChan(t types.Type) bool {
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func closesBefore(pass *analysis.Pass, fn ast.Node, loop *ast.ForStmt, v *types.Var) bool {
	closed := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if closed {
			return false
		}
		if n == nil || n.Pos() >= loop.Pos() {
			return n == nil
		}
		lit, ok := spawnedBody(n)
		if !ok {
			return true
		}
		if litCloses(pass, lit, v) {
			closed = true
		}
		return true
	})
	return closed
}

func spawnedBody(n ast.Node) (*ast.FuncLit, bool) {
	gs, ok := n.(*ast.GoStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(gs.Call).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
	if !ok {
		return nil, false
	}
	return lit, true
}

func litCloses(pass *analysis.Pass, lit *ast.FuncLit, v *types.Var) bool {
	closed := false
	walkNoLit(lit.Body, func(x ast.Node) bool {
		if closesVar(pass, x, v) {
			closed = true
		}
		return !closed
	})
	return closed
}

func closesVar(pass *analysis.Pass, x ast.Node, v *types.Var) bool {
	c, ok := x.(*ast.CallExpr)
	if !ok || !pass.TypesInfo.Types[c.Fun].IsBuiltin() {
		return false
	}
	if len(c.Args) != 1 {
		return false
	}
	id, ok := ast.Unparen(c.Args[0]).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == types.Object(v)
}
