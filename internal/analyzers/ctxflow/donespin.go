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
	Doc:      "report a for loop with no init or post whose only statement is a select with one context Done receive and a default that neither blocks nor leaves the loop; a default path that can block is not reported and only one communication clause is checked",
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
	if !ok || !receivesDone(pass.TypesInfo, comm.Comm) || defaultBlocks(pass.TypesInfo, dflt) {
		return
	}
	if !directlyInFor(cur) {
		return
	}
	pass.Report(analysis.Diagnostic{Pos: sel.Pos(), End: sel.End(), Message: donespinMessage})
}

func directlyInFor(cur inspector.Cursor) bool {
	block, ok := cur.Parent().Node().(*ast.BlockStmt)
	if !ok {
		return false
	}
	if len(block.List) != 1 {
		return false
	}
	n, ok := cur.Parent().Parent().Node().(*ast.ForStmt)
	if !ok || n.Body != block {
		return false
	}
	return n.Init == nil && n.Post == nil
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

func defaultBlocks(info *types.Info, dflt *ast.CommClause) bool {
	for _, s := range dflt.Body {
		if stmtBlocks(info, s) {
			return true
		}
	}
	return false
}

type blockWalk struct {
	info    *types.Info
	blocked bool
}

func stmtBlocks(info *types.Info, s ast.Stmt) bool {
	w := &blockWalk{info: info}
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
	if leavesLoop(n) {
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
	if !ok || !hasDefault(sel) {
		return false
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

func leavesLoop(n ast.Node) bool {
	if _, ok := n.(*ast.ReturnStmt); ok {
		return true
	}
	b, ok := n.(*ast.BranchStmt)
	if !ok {
		return false
	}
	if b.Tok == token.GOTO {
		return true
	}
	return b.Tok == token.BREAK && b.Label != nil
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
	if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
		return true
	}
	call, ok := n.(*ast.CallExpr)
	return ok && !isTypeConversion(info, call) && !isLenOrCap(info, call)
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
