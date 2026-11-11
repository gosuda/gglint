package chanstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gosuda/gglint/internal/flow"
)

const boundCap = int64(1 << 60)

type boundWalker struct {
	info    *types.Info
	key     flow.Key
	senders map[*ast.FuncLit]bool
	trips   []int64
	loopBad []bool
	badLoop int
	lits    []*ast.FuncLit
	total   int64
	unknown bool
}

func backwardLabels(fn ast.Node) map[string]bool {
	labelPos := map[string][]token.Pos{}
	gotoPos := map[string][]token.Pos{}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.LabeledStmt:
			labelPos[n.Label.Name] = append(labelPos[n.Label.Name], n.Pos())
		case *ast.BranchStmt:
			if n.Tok == token.GOTO && n.Label != nil {
				gotoPos[n.Label.Name] = append(gotoPos[n.Label.Name], n.Pos())
			}
		}
		return true
	})
	out := map[string]bool{}
	for name, lps := range labelPos {
		if hasBackwardJump(lps, gotoPos[name]) {
			out[name] = true
		}
	}
	return out
}

func hasBackwardJump(lps, gps []token.Pos) bool {
	for _, lp := range lps {
		for _, gp := range gps {
			if gp > lp {
				return true
			}
		}
	}
	return false
}

func (w *boundWalker) walkStmts(list []ast.Stmt) {
	for _, s := range list {
		if w.unknown {
			return
		}
		w.walkStmt(s)
	}
}

func (w *boundWalker) walkStmt(s ast.Stmt) {
	if w.unknown || s == nil {
		return
	}
	if w.walkStmtControl(s) {
		return
	}
	w.walkStmtLeaf(s)
}

func (w *boundWalker) walkStmtControl(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.BlockStmt:
		w.walkStmts(s.List)
	case *ast.LabeledStmt:
		w.walkLabeled(s)
	case *ast.IfStmt:
		w.walkIf(s)
	case *ast.ForStmt:
		w.walkFor(s)
	case *ast.RangeStmt:
		w.walkRange(s)
	default:
		return false
	}
	return true
}

func (w *boundWalker) walkStmtLeaf(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.SendStmt:
		w.walkExpr(s.Chan)
		w.walkExpr(s.Value)
	case *ast.AssignStmt:
		w.walkExprs(s.Lhs)
		w.walkExprs(s.Rhs)
	case *ast.ReturnStmt:
		w.walkExprs(s.Results)
	case *ast.ExprStmt:
		w.walkExpr(s.X)
	case *ast.IncDecStmt:
		w.walkExpr(s.X)
	case *ast.DeclStmt:
		w.walkDecl(s)
	default:
		w.walkStmtCompound(s)
	}
}

func (w *boundWalker) walkStmtCompound(s ast.Stmt) {
	if w.walkStmtBranch(s) {
		return
	}
	w.walkStmtSpawn(s)
}

func (w *boundWalker) walkStmtBranch(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		w.walkStmt(s.Init)
		w.walkExpr(s.Tag)
		w.walkStmt(s.Body)
	case *ast.TypeSwitchStmt:
		w.walkStmt(s.Init)
		w.walkStmt(s.Assign)
		w.walkStmt(s.Body)
	case *ast.SelectStmt:
		w.walkStmt(s.Body)
	case *ast.CaseClause:
		w.walkExprs(s.List)
		w.walkStmts(s.Body)
	case *ast.CommClause:
		w.walkStmt(s.Comm)
		w.walkStmts(s.Body)
	default:
		return false
	}
	return true
}

func (w *boundWalker) walkStmtSpawn(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.DeferStmt:
		w.walkExpr(s.Call)
	case *ast.GoStmt:
		w.walkGo(s)
	case *ast.BranchStmt, *ast.EmptyStmt:
	default:
		w.markUnknownOnRecv(s)
	}
}

func (w *boundWalker) walkLabeled(s *ast.LabeledStmt) {
	w.walkStmt(s.Stmt)
}

func (w *boundWalker) walkIf(s *ast.IfStmt) {
	w.walkStmt(s.Init)
	w.walkExpr(s.Cond)
	w.walkStmt(s.Body)
	w.walkStmt(s.Else)
}

func (w *boundWalker) walkFor(s *ast.ForStmt) {
	trip, ok := loopTrip(w.info, s)
	w.enterLoop(trip, ok)
	w.walkStmt(s.Init)
	w.walkExpr(s.Cond)
	w.walkStmt(s.Post)
	w.walkStmt(s.Body)
	w.exitLoop()
}

func (w *boundWalker) walkRange(s *ast.RangeStmt) {
	trip, ok := loopTrip(w.info, s)
	w.enterLoop(trip, ok)
	w.walkExpr(s.X)
	w.walkExpr(s.Key)
	w.walkExpr(s.Value)
	w.walkStmt(s.Body)
	w.exitLoop()
}

func (w *boundWalker) enterLoop(trip int64, ok bool) {
	if !ok {
		w.loopBad = append(w.loopBad, true)
		w.trips = append(w.trips, 1)
		w.badLoop++
		return
	}
	w.loopBad = append(w.loopBad, false)
	w.trips = append(w.trips, trip)
}

func (w *boundWalker) exitLoop() {
	n := len(w.trips)
	if n == 0 {
		return
	}
	if w.loopBad[n-1] {
		w.badLoop--
	}
	w.trips = w.trips[:n-1]
	w.loopBad = w.loopBad[:n-1]
}

func (w *boundWalker) walkDecl(s *ast.DeclStmt) {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		w.walkExprs(vs.Values)
	}
}

func (w *boundWalker) walkGo(s *ast.GoStmt) {
	call := s.Call
	for _, a := range call.Args {
		w.walkExpr(a)
	}
	if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		w.walkLit(lit)
		return
	}
	w.walkExpr(call.Fun)
}

func (w *boundWalker) walkLit(lit *ast.FuncLit) {
	w.lits = append(w.lits, lit)
	w.walkStmt(lit.Body)
	w.lits = w.lits[:len(w.lits)-1]
}

func (w *boundWalker) walkExprs(list []ast.Expr) {
	for _, x := range list {
		w.walkExpr(x)
	}
}

func (w *boundWalker) walkExpr(x ast.Expr) {
	if w.unknown || x == nil {
		return
	}
	switch x := x.(type) {
	case *ast.UnaryExpr:
		w.walkUnary(x)
	case *ast.FuncLit:
		w.walkLit(x)
	case *ast.CallExpr:
		w.walkCall(x)
	case *ast.BinaryExpr:
		w.walkExpr(x.X)
		w.walkExpr(x.Y)
	case *ast.ParenExpr:
		w.walkExpr(x.X)
	default:
		w.walkExprOther(x)
	}
}

func (w *boundWalker) walkExprOther(x ast.Expr) {
	switch x := x.(type) {
	case *ast.StarExpr:
		w.walkExpr(x.X)
	case *ast.SelectorExpr:
		w.walkExpr(x.X)
	case *ast.TypeAssertExpr:
		w.walkExpr(x.X)
	case *ast.IndexExpr:
		w.walkExpr(x.X)
		w.walkExpr(x.Index)
	case *ast.SliceExpr:
		w.walkSlice(x)
	default:
		w.walkExprFinal(x)
	}
}

func (w *boundWalker) walkSlice(x *ast.SliceExpr) {
	w.walkExpr(x.X)
	w.walkExpr(x.Low)
	w.walkExpr(x.High)
	w.walkExpr(x.Max)
}

func (w *boundWalker) walkExprFinal(x ast.Expr) {
	switch x := x.(type) {
	case *ast.IndexListExpr:
		w.walkExpr(x.X)
		w.walkExprs(x.Indices)
	case *ast.CompositeLit:
		w.walkExprs(x.Elts)
	case *ast.KeyValueExpr:
		w.walkExpr(x.Key)
		w.walkExpr(x.Value)
	case *ast.Ellipsis:
		w.walkExpr(x.Elt)
	case *ast.Ident, *ast.BasicLit:
	default:
		w.markUnknownOnRecv(x)
	}
}

func (w *boundWalker) walkUnary(x *ast.UnaryExpr) {
	if x.Op == token.ARROW {
		if k, ok := flow.KeyOf(w.info, x.X); ok && k == w.key {
			w.addSite()
			return
		}
	}
	w.walkExpr(x.X)
}

func (w *boundWalker) walkCall(c *ast.CallExpr) {
	if isUnevaluatedUnsafe(w.info, c) {
		return
	}
	w.walkExpr(c.Fun)
	w.walkExprs(c.Args)
}

func (w *boundWalker) addSite() {
	if w.badLoop > 0 {
		w.unknown = true
		return
	}
	for _, lit := range w.lits {
		if !w.senders[lit] {
			w.unknown = true
			return
		}
	}
	mult := int64(1)
	for _, t := range w.trips {
		if t <= 0 {
			return
		}
		if mult > boundCap/t {
			mult = boundCap
			break
		}
		mult *= t
	}
	if w.total > boundCap-mult {
		w.total = boundCap
		return
	}
	w.total += mult
}

func (w *boundWalker) markUnknownOnRecv(n ast.Node) {
	ast.Inspect(n, func(x ast.Node) bool {
		if w.unknown {
			return false
		}
		if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
			if k, ok := flow.KeyOf(w.info, u.X); ok && k == w.key {
				w.unknown = true
				return false
			}
		}
		return true
	})
}

func upperBoundRecvsCached(ctx *fnCtx, k flow.Key, senders map[*ast.FuncLit]bool) (int64, bool) {
	body := flow.Body(ctx.fn)
	if body == nil {
		return 0, true
	}
	if len(ctx.backward) > 0 {
		return 0, false
	}
	if ctx.facts.hasAnyRange(k) {
		return 0, false
	}
	w := &boundWalker{
		info:    ctx.info,
		key:     k,
		senders: senders,
	}
	w.walkStmts(body.List)
	if w.unknown {
		return 0, false
	}
	return w.total, true
}
