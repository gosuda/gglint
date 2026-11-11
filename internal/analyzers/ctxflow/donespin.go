package ctxflow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/inspection"
)

const donespinMessage = "this loop polls the context with a default case and never blocks, so it spins: remove the default case, or add a timer case"

var donespinAnalyzer = &analysis.Analyzer{
	Name:     "donespin",
	Doc:      "report a for loop with no init or post whose only statement is a select with one context Done receive and a default that neither blocks nor leaves the loop; a default path that can block is not reported and only one communication clause is checked; a loop whose condition variable the default branch assigns on every pass is not reported",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runDonespin,
}

func runDonespin(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.SelectStmt)(nil)) {
		checkDonespin(pass, cur)
	}
	return nil, nil
}

func checkDonespin(pass *analysis.Pass, cur inspector.Cursor) {
	sel, ok := cur.Node().(*ast.SelectStmt)
	if !ok {
		return
	}
	comm, dflt, ok := splitDonespin(sel)
	if !ok || !receivesDone(pass.TypesInfo, comm.Comm) {
		return
	}
	loop, ok := pollingLoop(pass, cur, dflt)
	if !ok {
		return
	}
	if defaultBlocks(pass.TypesInfo, dflt, loop, labelsOf(cur)) {
		return
	}
	pass.Report(analysis.Diagnostic{Pos: sel.Pos(), End: sel.End(), Message: donespinMessage})
}

func pollingLoop(pass *analysis.Pass, cur inspector.Cursor, dflt *ast.CommClause) (*ast.ForStmt, bool) {
	block, ok := cur.Parent().Node().(*ast.BlockStmt)
	if !ok || len(block.List) != 1 {
		return nil, false
	}
	n, ok := cur.Parent().Parent().Node().(*ast.ForStmt)
	if !ok || n.Body != block || n.Init != nil || n.Post != nil {
		return nil, false
	}
	if headerCanEnd(pass, n, dflt) {
		return nil, false
	}
	return n, true
}

func headerCanEnd(pass *analysis.Pass, n *ast.ForStmt, dflt *ast.CommClause) bool {
	if n.Cond == nil {
		return false
	}
	assigned := everyPassAssigned(pass, n, dflt)
	return anyVarUsedIn(pass, n.Cond, assigned)
}

func everyPassAssigned(pass *analysis.Pass, n *ast.ForStmt, dflt *ast.CommClause) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	for _, s := range dflt.Body {
		directAssigned(pass, s, out)
	}
	for _, s := range n.Body.List {
		if _, ok := s.(*ast.SelectStmt); ok {
			continue
		}
		directAssigned(pass, s, out)
	}
	return out
}

func directAssigned(pass *analysis.Pass, s ast.Stmt, into map[*types.Var]bool) {
	switch s := s.(type) {
	case *ast.AssignStmt:
		for _, lhs := range s.Lhs {
			addAssignedVar(pass, lhs, into)
		}
	case *ast.IncDecStmt:
		addAssignedVar(pass, s.X, into)
	}
}

func addAssignedVar(pass *analysis.Pass, e ast.Expr, into map[*types.Var]bool) {
	if id, ok := stripParens(e).(*ast.Ident); ok {
		if v := varOf(pass.TypesInfo, id); v != nil {
			into[v] = true
		}
	}
}

func anyVarUsedIn(pass *analysis.Pass, cond ast.Expr, vars map[*types.Var]bool) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v := varOf(pass.TypesInfo, id); v != nil && vars[v] {
			found = true
			return false
		}
		return true
	})
	return found
}

func labelsOf(cur inspector.Cursor) map[string]*ast.LabeledStmt {
	out := map[string]*ast.LabeledStmt{}
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		root := fn.Node()
		ast.Inspect(root, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok && lit != root {
				return false
			}
			if l, ok := n.(*ast.LabeledStmt); ok && l.Label != nil {
				if _, dup := out[l.Label.Name]; !dup {
					out[l.Label.Name] = l
				}
			}
			return true
		})
		return out
	}
	return out
}

func splitDonespin(sel *ast.SelectStmt) (commClause, dfltClause *ast.CommClause, found bool) {
	if sel.Body == nil || len(sel.Body.List) != 2 {
		return nil, nil, false
	}
	for _, st := range sel.Body.List {
		cc, ok := st.(*ast.CommClause)
		if !ok {
			return nil, nil, false
		}
		if cc.Comm == nil {
			dfltClause = cc
		} else {
			commClause = cc
		}
	}
	if commClause == nil || dfltClause == nil {
		return nil, nil, false
	}
	return commClause, dfltClause, true
}

func receivesDone(info *types.Info, comm ast.Stmt) bool {
	switch n := comm.(type) {
	case *ast.ExprStmt:
		return recvDone(info, n.X)
	case *ast.AssignStmt:
		for _, rhs := range n.Rhs {
			if recvDone(info, rhs) {
				return true
			}
		}
	}
	return false
}

func recvDone(info *types.Info, e ast.Expr) bool {
	u, ok := stripParens(e).(*ast.UnaryExpr)
	if !ok || u.Op != token.ARROW {
		return false
	}
	call, ok := stripParens(u.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	return isDoneCall(info, call)
}

func isDoneCall(info *types.Info, call *ast.CallExpr) bool {
	if len(call.Args) != 0 {
		return false
	}
	sel, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Done" {
		return false
	}
	named, ok := types.Unalias(info.TypeOf(sel.X)).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == contextName && obj.Pkg() != nil && obj.Pkg().Path() == contextPath
}

func defaultBlocks(info *types.Info, dflt *ast.CommClause, loop *ast.ForStmt, labels map[string]*ast.LabeledStmt) bool {
	for _, s := range dflt.Body {
		if stmtBlocks(info, s, loop, labels) {
			return true
		}
	}
	return false
}

type blockWalk struct {
	info    *types.Info
	loop    *ast.ForStmt
	labels  map[string]*ast.LabeledStmt
	blocked bool
}

func stmtBlocks(info *types.Info, s ast.Stmt, loop *ast.ForStmt, labels map[string]*ast.LabeledStmt) bool {
	w := &blockWalk{info: info, loop: loop, labels: labels}
	w.walk(s)
	return w.blocked
}

func (w *blockWalk) walk(n ast.Node) {
	if w.blocked || n == nil {
		return
	}
	if _, ok := n.(*ast.FuncLit); ok {
		return
	}
	if w.leavesLoop(n) {
		w.blocked = true
		return
	}
	if w.walkSelect(n) {
		return
	}
	if isBlockingOp(w.info, n) {
		w.blocked = true
		return
	}
	w.walkChildren(n)
}

func (w *blockWalk) walkSelect(n ast.Node) bool {
	sel, ok := n.(*ast.SelectStmt)
	if !ok {
		return false
	}
	if !hasDefault(sel) {
		w.blocked = true
		return true
	}
	w.walkDefaultBody(sel)
	return true
}

func (w *blockWalk) walkChildren(n ast.Node) {
	for _, child := range childrenOf(n) {
		w.walk(child)
		if w.blocked {
			return
		}
	}
}

func (w *blockWalk) leavesLoop(n ast.Node) bool {
	if _, ok := n.(*ast.ReturnStmt); ok {
		return true
	}
	b, ok := n.(*ast.BranchStmt)
	if !ok {
		return false
	}
	if b.Label == nil {
		return false
	}
	target, ok := w.labels[b.Label.Name]
	if !ok {
		return false
	}
	if b.Tok == token.GOTO {
		return outsideLoop(target, w.loop)
	}
	if b.Tok == token.CONTINUE {
		return target.Stmt != w.loop && wrapsLoop(target, w.loop)
	}
	if b.Tok != token.BREAK {
		return false
	}
	return wrapsLoop(target, w.loop)
}

func wrapsLoop(target *ast.LabeledStmt, loop *ast.ForStmt) bool {
	return target.Pos() <= loop.Pos() && loop.End() <= target.End()
}

func outsideLoop(target *ast.LabeledStmt, loop *ast.ForStmt) bool {
	if target.Pos() >= loop.Pos() && target.End() <= loop.End() {
		return false
	}
	if target.Stmt.Pos() == loop.Pos() && target.Stmt.End() == loop.End() {
		return false
	}
	return true
}

func (w *blockWalk) walkDefaultBody(sel *ast.SelectStmt) {
	for _, st := range sel.Body.List {
		cc, ok := st.(*ast.CommClause)
		if !ok {
			continue
		}
		for _, b := range cc.Body {
			w.walk(b)
			if w.blocked {
				return
			}
		}
	}
}

func hasDefault(sel *ast.SelectStmt) bool {
	if sel.Body == nil {
		return false
	}
	for _, st := range sel.Body.List {
		if cc, ok := st.(*ast.CommClause); ok && cc.Comm == nil {
			return true
		}
	}
	return false
}

func childrenOf(n ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(n, func(c ast.Node) bool {
		if c == nil || c == n {
			return true
		}
		out = append(out, c)
		return false
	})
	return out
}

func isBlockingOp(info *types.Info, n ast.Node) bool {
	if _, ok := n.(*ast.SendStmt); ok {
		return true
	}
	if r, ok := n.(*ast.RangeStmt); ok && isChannelType(info.TypeOf(r.X)) {
		return true
	}
	if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
		return true
	}
	call, ok := n.(*ast.CallExpr)
	return ok && !isTypeConversion(info, call) && !isLenOrCap(info, call)
}

func isChannelType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func isTypeConversion(info *types.Info, call *ast.CallExpr) bool {
	tv, ok := info.Types[stripParens(call.Fun)]
	return ok && tv.IsType()
}

func isLenOrCap(info *types.Info, call *ast.CallExpr) bool {
	id, ok := stripParens(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.ObjectOf(id).(*types.Builtin)
	return ok && (b.Name() == "len" || b.Name() == "cap")
}
