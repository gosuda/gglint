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
	Doc:      "report a continue directly in if err != nil that skips the advance when the advance follows the if and none precedes it; spin covers five shapes only",
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
		site := continueSite{loop: loop, list: loop.Body.List, check: check, vars: vars, at: j, errs: condVars(pass.TypesInfo, check.Cond)}
		checkContinues(pass, site, label)
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

type continueSite struct {
	loop  *ast.ForStmt
	list  []ast.Stmt
	check *ast.IfStmt
	vars  map[*types.Var]bool
	at    int
	errs  map[*types.Var]bool
}

func checkContinues(pass *analysis.Pass, site continueSite, label string) {
	for k, inner := range site.check.Body.List {
		reportContinue(pass, site, label, k, inner)
	}
}

func reportContinue(pass *analysis.Pass, site continueSite, label string, k int, inner ast.Stmt) {
	branch, ok := inner.(*ast.BranchStmt)
	if !ok || branch.Tok != token.CONTINUE {
		return
	}
	if branch.Label != nil && (label == "" || branch.Label.Name != label) {
		return
	}
	info := pass.TypesInfo
	if advancedBefore(info, site.list, site.at, site.check, k, site.vars) {
		return
	}
	if outerAdvanced(info, site, k) {
		return
	}
	if advancedAfter(info, site.list, site.at, site.vars) {
		pass.Reportf(branch.Pos(), "this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue")
	}
}

func outerAdvanced(info *types.Info, site continueSite, k int) bool {
	written := map[*types.Var]bool{}
	for _, stmt := range site.list[:site.at] {
		collectOuterWrites(info, site.loop, stmt, site.errs, written)
	}
	if site.check.Init != nil {
		collectOuterWrites(info, site.loop, site.check.Init, site.errs, written)
	}
	for _, stmt := range site.check.Body.List[:k] {
		collectOuterWrites(info, site.loop, stmt, site.errs, written)
	}
	if len(written) == 0 {
		return false
	}
	for _, stmt := range site.list[:site.at] {
		if stmtReadsWritten(info, site.loop, stmt, site.errs, written) {
			return true
		}
	}
	if site.check.Init != nil && stmtReadsWritten(info, site.loop, site.check.Init, site.errs, written) {
		return true
	}
	return condReadsWritten(info, site.check.Cond, written)
}

func collectOuterWrites(info *types.Info, loop *ast.ForStmt, node ast.Node, errs, out map[*types.Var]bool) {
	walkNoLit(node, func(n ast.Node) bool {
		addOuterNode(info, loop, n, errs, out)
		return true
	})
}

func addOuterNode(info *types.Info, loop *ast.ForStmt, n ast.Node, errs, out map[*types.Var]bool) {
	switch t := n.(type) {
	case *ast.AssignStmt:
		addOuterLHS(info, loop, t.Lhs, errs, out)
	case *ast.IncDecStmt:
		addOuterOne(info, loop, t.X, errs, out)
	case *ast.RangeStmt:
		addOuterRange(info, loop, t, errs, out)
	}
}

func addOuterLHS(info *types.Info, loop *ast.ForStmt, lhs []ast.Expr, errs, out map[*types.Var]bool) {
	for _, e := range lhs {
		addOuterOne(info, loop, e, errs, out)
	}
}

func addOuterOne(info *types.Info, loop *ast.ForStmt, e ast.Expr, errs, out map[*types.Var]bool) {
	if v, ok := outerRootVar(info, loop, e, errs); ok {
		out[v] = true
	}
}

func addOuterRange(info *types.Info, loop *ast.ForStmt, t *ast.RangeStmt, errs, out map[*types.Var]bool) {
	if t.Tok != token.ASSIGN {
		return
	}
	addOuterOne(info, loop, t.Key, errs, out)
	addOuterOne(info, loop, t.Value, errs, out)
}

func outerRootVar(info *types.Info, loop *ast.ForStmt, e ast.Expr, errs map[*types.Var]bool) (*types.Var, bool) {
	if e == nil {
		return nil, false
	}
	id := rootIdent(e)
	if id == nil {
		return nil, false
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok || errs[v] {
		return nil, false
	}
	if v.Parent() == nil {
		return v, true
	}
	if v.Pos() < loop.Pos() || v.Pos() >= loop.End() {
		return v, true
	}
	return nil, false
}

func stmtReadsWritten(info *types.Info, loop *ast.ForStmt, node ast.Node, errs, written map[*types.Var]bool) bool {
	here := map[*types.Var]bool{}
	collectOuterWrites(info, loop, node, errs, here)
	found := false
	walkNoLit(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := info.Uses[id].(*types.Var)
		if !ok || !written[v] || here[v] {
			return true
		}
		found = true
		return false
	})
	return found
}

func condReadsWritten(info *types.Info, cond ast.Expr, written map[*types.Var]bool) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v, ok := info.Uses[id].(*types.Var); ok && written[v] {
			found = true
			return false
		}
		return true
	})
	return found
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
