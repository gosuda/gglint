package spin

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var skipadvanceAnalyzer = &analysis.Analyzer{
	Name:     "skipadvance",
	Doc:      "report continue statements skipping the loop advance; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runSkipadvance,
}

func runSkipadvance(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkSkipadvance(pass, cur, loop)
	})
	return nil, nil
}

func checkSkipadvance(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) {
	if loop.Cond == nil || loop.Post != nil || loop.Body == nil {
		return
	}
	vars := condVars(pass.TypesInfo, loop.Cond)
	label := loopLabel(cur)
	for j, stmt := range loop.Body.List {
		check, ok := stmt.(*ast.IfStmt)
		if !ok || !errorCompared(pass.TypesInfo, check.Cond) {
			continue
		}
		checkContinues(pass, loop.Body.List, j, check, vars, label)
	}
}

func loopLabel(cur inspector.Cursor) string {
	if labeled, ok := cur.Parent().Node().(*ast.LabeledStmt); ok {
		return labeled.Label.Name
	}
	return ""
}

func condVars(info *types.Info, cond ast.Expr) map[*types.Var]bool {
	vars := map[*types.Var]bool{}
	ast.Inspect(cond, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok {
				vars[v] = true
			}
		}
		return true
	})
	return vars
}

func checkContinues(pass *analysis.Pass, list []ast.Stmt, j int, check *ast.IfStmt, vars map[*types.Var]bool, label string) {
	info := pass.TypesInfo
	for k, inner := range check.Body.List {
		branch, ok := inner.(*ast.BranchStmt)
		if !ok || branch.Tok != token.CONTINUE {
			continue
		}
		if branch.Label != nil && (label == "" || branch.Label.Name != label) {
			continue
		}
		if advancedBefore(info, list, j, check, k, vars) {
			continue
		}
		if advancedAfter(info, list, j, vars) {
			pass.Reportf(branch.Pos(), "this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue")
		}
	}
}

func errorCompared(info *types.Info, cond ast.Expr) bool {
	cmp, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || cmp.Op != token.NEQ {
		return false
	}
	return isNil(info, cmp.X) && isErrorType(info.TypeOf(cmp.Y)) ||
		isNil(info, cmp.Y) && isErrorType(info.TypeOf(cmp.X))
}

func advancedBefore(info *types.Info, list []ast.Stmt, j int, check *ast.IfStmt, k int, vars map[*types.Var]bool) bool {
	for _, stmt := range list[:j] {
		if stmtAssigns(info, stmt, vars) {
			return true
		}
	}
	if check.Init != nil && stmtAssigns(info, check.Init, vars) {
		return true
	}
	for _, stmt := range check.Body.List[:k] {
		if stmtAssigns(info, stmt, vars) {
			return true
		}
	}
	return false
}

func advancedAfter(info *types.Info, list []ast.Stmt, j int, vars map[*types.Var]bool) bool {
	for _, stmt := range list[j+1:] {
		if stmtAssigns(info, stmt, vars) {
			return true
		}
	}
	return false
}

func stmtAssigns(info *types.Info, stmt ast.Stmt, vars map[*types.Var]bool) bool {
	hit := false
	walkNoLit(stmt, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			hit = assignHits(info, t.Lhs, vars) || hit
		case *ast.IncDecStmt:
			hit = varIn(info, t.X, vars) || hit
		case *ast.RangeStmt:
			hit = rangeAssignHits(info, t, vars) || hit
		}
		return !hit
	})
	return hit
}

func assignHits(info *types.Info, lhs []ast.Expr, vars map[*types.Var]bool) bool {
	for _, e := range lhs {
		if varIn(info, e, vars) {
			return true
		}
	}
	return false
}

func rangeAssignHits(info *types.Info, t *ast.RangeStmt, vars map[*types.Var]bool) bool {
	if t.Tok != token.ASSIGN {
		return false
	}
	return rangeVar(info, t.Key, vars) || rangeVar(info, t.Value, vars)
}

func rangeVar(info *types.Info, e ast.Expr, vars map[*types.Var]bool) bool {
	if e == nil {
		return false
	}
	return varIn(info, e, vars)
}

func varIn(info *types.Info, e ast.Expr, vars map[*types.Var]bool) bool {
	id := rootIdent(e)
	if id == nil {
		return false
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	return ok && vars[v]
}
