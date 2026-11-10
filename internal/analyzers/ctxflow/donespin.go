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
	Doc:      "report a loop that polls a context Done channel with a non-blocking default",
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
	switch n := cur.Parent().Parent().Node().(type) {
	case *ast.ForStmt:
		return n.Body == block
	case *ast.RangeStmt:
		return n.Body == block
	}
	return false
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
	if sel, ok := n.(*ast.SelectStmt); ok && hasDefault(sel) {
		w.walkDefaultBody(sel)
		return
	}
	if isBlockingOp(w.info, n) {
		w.blocked = true
		return
	}
	for _, child := range childrenOf(n) {
		w.walk(child)
		if w.blocked {
			return
		}
	}
}

func (w *blockWalk) walkDefaultBody(sel *ast.SelectStmt) {
	for _, st := range sel.Body.List {
		cc, ok := st.(*ast.CommClause)
		if !ok || cc.Comm != nil {
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
