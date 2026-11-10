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
	sel, def, comms, ok := pollSelect(loop)
	if !ok {
		return
	}
	if defaultSpins(pass, def) {
		return
	}
	if comms == 1 && doneRecv(pass, sel) {
		return
	}
	pass.Reportf(loop.Pos(), "no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it")
}

func pollSelect(loop *ast.ForStmt) (*ast.SelectStmt, *ast.CommClause, int, bool) {
	if loop.Body == nil || len(loop.Body.List) != 1 {
		return nil, nil, 0, false
	}
	sel, ok := loop.Body.List[0].(*ast.SelectStmt)
	if !ok {
		return nil, nil, 0, false
	}
	def, comms, ok := selectSplit(sel)
	if !ok || def == nil || len(def.Body) == 0 {
		return nil, nil, 0, false
	}
	return sel, def, comms, true
}

func defaultSpins(pass *analysis.Pass, def *ast.CommClause) bool {
	for _, stmt := range def.Body {
		if defaultWaits(pass, stmt) {
			return true
		}
	}
	return false
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

func defaultWaits(pass *analysis.Pass, stmt ast.Stmt) bool {
	waits := false
	walkNoLit(stmt, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CallExpr:
			if isCodeCall(pass.TypesInfo, t) {
				waits = true
			}
		case *ast.SendStmt:
			waits = true
		case *ast.UnaryExpr:
			if t.Op == token.ARROW {
				waits = true
			}
		case *ast.SelectStmt:
			waits = true
		}
		return !waits
	})
	return waits
}

func doneRecv(pass *analysis.Pass, sel *ast.SelectStmt) bool {
	for _, stmt := range sel.Body.List {
		clause, ok := stmt.(*ast.CommClause)
		if !ok || clause.Comm == nil {
			continue
		}
		if clauseDoneRecv(pass, clause.Comm) {
			return true
		}
	}
	return false
}

func clauseDoneRecv(pass *analysis.Pass, comm ast.Node) bool {
	found := false
	walkNoLit(comm, func(n ast.Node) bool {
		if isDoneRecv(pass, n) {
			found = true
		}
		return !found
	})
	return found
}

func isDoneRecv(pass *analysis.Pass, n ast.Node) bool {
	recv, ok := n.(*ast.UnaryExpr)
	if !ok || recv.Op != token.ARROW {
		return false
	}
	call, ok := ast.Unparen(recv.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return ok && len(call.Args) == 0 && isPkgMethod(pass.TypesInfo, fn, "context", "Done")
}
