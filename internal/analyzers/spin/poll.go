package spin

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var pollAnalyzer = &analysis.Analyzer{
	Name:     "poll",
	Doc:      "report for-select loops whose default case never waits; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runPoll,
}

func runPoll(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(_ inspector.Cursor, loop *ast.ForStmt) {
		checkPoll(pass, loop)
	})
	return nil, nil
}

func checkPoll(pass *analysis.Pass, loop *ast.ForStmt) {
	if loop.Body == nil || len(loop.Body.List) != 1 {
		return
	}
	sel, ok := loop.Body.List[0].(*ast.SelectStmt)
	if !ok {
		return
	}
	def, comms, ok := selectSplit(sel)
	if !ok || def == nil || comms == 0 || len(def.Body) == 0 {
		return
	}
	for _, stmt := range def.Body {
		if defaultWaits(stmt) {
			return
		}
	}
	if comms == 1 && doneRecv(pass, sel) {
		return
	}
	pass.Reportf(loop.Pos(), "no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it")
}

func selectSplit(sel *ast.SelectStmt) (*ast.CommClause, int, bool) {
	var def *ast.CommClause
	comms := 0
	for _, stmt := range sel.Body.List {
		clause, ok := stmt.(*ast.CommClause)
		if !ok {
			return nil, 0, false
		}
		if clause.Comm == nil {
			def = clause
		} else {
			comms++
		}
	}
	return def, comms, true
}

func defaultWaits(stmt ast.Stmt) bool {
	waits := false
	walkNoLit(stmt, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CallExpr:
			waits = true
		case *ast.SendStmt:
			waits = true
		case *ast.UnaryExpr:
			waits = t.Op == token.ARROW
		case *ast.SelectStmt:
			waits = true
		}
		return !waits
	})
	return waits
}

func doneRecv(pass *analysis.Pass, sel *ast.SelectStmt) bool {
	found := false
	for _, stmt := range sel.Body.List {
		clause, ok := stmt.(*ast.CommClause)
		if !ok || clause.Comm == nil {
			continue
		}
		walkNoLit(clause.Comm, func(n ast.Node) bool {
			recv, ok := n.(*ast.UnaryExpr)
			if !ok || recv.Op != token.ARROW {
				return true
			}
			call, ok := ast.Unparen(recv.X).(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if ok && len(call.Args) == 0 && isPkgMethod(pass.TypesInfo, fn, "context", "Done") {
				found = true
			}
			return true
		})
	}
	return found
}
