package spin

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var pollAnalyzer = &analysis.Analyzer{
	Name:     "poll",
	Doc:      "report a for loop whose body is one select with a default that never waits and never leaves; counted loops that keep their counter and Done-only selects are skipped; a call in the default clause is assumed to wait, as is a channel operation or a call outside sync/atomic in the condition or the post statement of the loop; spin covers five shapes only",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runPoll,
}

func runPoll(pass *analysis.Pass) (any, error) {
	forLoops(pass, func(cur inspector.Cursor, loop *ast.ForStmt) {
		checkPoll(pass, cur, loop)
	})
	return nil, nil
}

func checkPoll(pass *analysis.Pass, cur inspector.Cursor, loop *ast.ForStmt) {
	sel, def, comms, ok := pollSelect(loop)
	if !ok {
		return
	}
	if isCounted(pass.TypesInfo, loop) || headerWaits(pass, loop) {
		return
	}
	if defaultSpins(pass, def) {
		return
	}
	if defaultExits(pass, cur, def) {
		return
	}
	if comms == 1 && doneRecv(pass, sel) {
		return
	}
	pass.Reportf(loop.Pos(), "no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it")
}

func headerWaits(pass *analysis.Pass, loop *ast.ForStmt) bool {
	return headerPartWaits(pass, loop.Cond) || headerPartWaits(pass, loop.Post)
}

func headerPartWaits(pass *analysis.Pass, part ast.Node) bool {
	if part == nil {
		return false
	}
	waits := false
	walkNoLit(part, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isAtomicCall(pass.TypesInfo, call) {
			return false
		}
		waits = waits || pollNodeWaits(pass, n)
		return !waits
	})
	return waits
}

func isAtomicCall(info *types.Info, call *ast.CallExpr) bool {
	var name *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		name = fun.Sel
	case *ast.Ident:
		name = fun
	default:
		return false
	}
	fn, ok := info.Uses[name].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "sync/atomic"
}

func isCounted(info *types.Info, loop *ast.ForStmt) bool {
	if loop.Init == nil || loop.Cond == nil || loop.Post == nil {
		return false
	}
	inits := assignedVars(info, loop.Init)
	if len(inits) == 0 {
		return false
	}
	posts := assignedVars(info, loop.Post)
	for v := range inits {
		if isCounter(info, loop, posts, v) {
			return true
		}
	}
	return false
}

func isCounter(info *types.Info, loop *ast.ForStmt, posts map[*types.Var]bool, v *types.Var) bool {
	if !posts[v] {
		return false
	}
	if !condMentions(info, loop.Cond, v) {
		return false
	}
	return loop.Body == nil || !assignsRoot(info, loop.Body, v)
}

func assignedVars(info *types.Info, node ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	switch t := node.(type) {
	case *ast.AssignStmt:
		addAssignVars(info, t, out)
	case *ast.IncDecStmt:
		addIncDecVar(info, t, out)
	case *ast.DeclStmt:
		addDeclVars(info, t, out)
	}
	return out
}

func addAssignVars(info *types.Info, t *ast.AssignStmt, out map[*types.Var]bool) {
	for _, e := range t.Lhs {
		addRootVar(info, e, out)
	}
}

func addIncDecVar(info *types.Info, t *ast.IncDecStmt, out map[*types.Var]bool) {
	addRootVar(info, t.X, out)
}

func addRootVar(info *types.Info, e ast.Expr, out map[*types.Var]bool) {
	id := rootIdent(e)
	if id == nil {
		return
	}
	if v, ok := info.ObjectOf(id).(*types.Var); ok {
		out[v] = true
	}
}

func addDeclVars(info *types.Info, t *ast.DeclStmt, out map[*types.Var]bool) {
	decl, ok := t.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range decl.Specs {
		addSpecVars(info, spec, out)
	}
}

func addSpecVars(info *types.Info, spec ast.Spec, out map[*types.Var]bool) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok {
		return
	}
	for _, name := range vs.Names {
		if v, ok := info.Defs[name].(*types.Var); ok {
			out[v] = true
		}
	}
}

func condMentions(info *types.Info, cond ast.Expr, v *types.Var) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if info.ObjectOf(id) == types.Object(v) {
			found = true
			return false
		}
		return true
	})
	return found
}

func defaultExits(pass *analysis.Pass, cur inspector.Cursor, def *ast.CommClause) bool {
	if len(def.Body) == 0 {
		return false
	}
	return pollLeaves(pass, cur, def.Body[len(def.Body)-1])
}

func pollLeaves(pass *analysis.Pass, cur inspector.Cursor, stmt ast.Stmt) bool {
	switch t := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return pollBranchLeaves(pass, cur, t)
	case *ast.ExprStmt:
		return isPanicCall(pass.TypesInfo, t.X)
	case *ast.BlockStmt:
		return pollBlockTailLeaves(pass, cur, t)
	case *ast.IfStmt:
		return pollIfLeaves(pass, cur, t)
	default:
		return pollStructuredLeaves(pass, cur, stmt)
	}
}

func pollStructuredLeaves(pass *analysis.Pass, cur inspector.Cursor, stmt ast.Stmt) bool {
	if body, label, ok := switchLabel(stmt); ok {
		return pollCaseLeaves(pass, cur, body, label)
	}
	if lab, ok := stmt.(*ast.LabeledStmt); ok {
		return pollLeaves(pass, cur, lab.Stmt)
	}
	return false
}

func switchLabel(stmt ast.Stmt) (*ast.BlockStmt, string, bool) {
	if lab, ok := stmt.(*ast.LabeledStmt); ok {
		if body := switchBody(lab.Stmt); body != nil {
			return body, lab.Label.Name, true
		}
		return nil, "", false
	}
	if body := switchBody(stmt); body != nil {
		return body, "", true
	}
	return nil, "", false
}

func switchBody(stmt ast.Stmt) *ast.BlockStmt {
	switch t := stmt.(type) {
	case *ast.SwitchStmt:
		return t.Body
	case *ast.TypeSwitchStmt:
		return t.Body
	default:
		return nil
	}
}

func pollBranchLeaves(pass *analysis.Pass, cur inspector.Cursor, t *ast.BranchStmt) bool {
	if t.Tok == token.GOTO {
		return pollGotoLeaves(pass, cur, t.Label)
	}
	if t.Tok == token.BREAK && t.Label != nil {
		return pollBreakLeaves(pass, cur, t.Label)
	}
	if t.Tok == token.CONTINUE && t.Label != nil {
		return !pollOwnLabelNames(cur)[t.Label.Name]
	}
	return false
}

func pollBreakLeaves(pass *analysis.Pass, cur inspector.Cursor, label *ast.Ident) bool {
	loop, ok := cur.Node().(*ast.ForStmt)
	if !ok {
		return false
	}
	target := findPollLabel(pass, cur, label)
	if target == nil {
		return false
	}
	return target.Pos() <= loop.Pos() && loop.End() <= target.End()
}

func pollGotoLeaves(pass *analysis.Pass, cur inspector.Cursor, label *ast.Ident) bool {
	if label == nil {
		return false
	}
	loop, ok := cur.Node().(*ast.ForStmt)
	if !ok || loop.Body == nil {
		return false
	}
	if pollOwnLabelNames(cur)[label.Name] {
		return false
	}
	target := findPollLabel(pass, cur, label)
	if target == nil {
		return false
	}
	return target.Pos() < loop.Body.Pos() || target.End() > loop.Body.End()
}

func findPollLabel(pass *analysis.Pass, cur inspector.Cursor, label *ast.Ident) *ast.LabeledStmt {
	obj := pass.TypesInfo.ObjectOf(label)
	if obj == nil {
		return nil
	}
	fn := enclosingFunc(cur)
	if fn == nil {
		return nil
	}
	var found *ast.LabeledStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		lab, ok := n.(*ast.LabeledStmt)
		if !ok {
			return true
		}
		if pass.TypesInfo.ObjectOf(lab.Label) == obj {
			found = lab
			return false
		}
		return true
	})
	return found
}

func pollOwnLabelNames(cur inspector.Cursor) map[string]bool {
	out := map[string]bool{}
	for p := cur; ; {
		lab, ok := p.Parent().Node().(*ast.LabeledStmt)
		if !ok {
			break
		}
		out[lab.Label.Name] = true
		p = p.Parent()
	}
	return out
}

func pollBlockTailLeaves(pass *analysis.Pass, cur inspector.Cursor, t *ast.BlockStmt) bool {
	if len(t.List) == 0 {
		return false
	}
	return pollLeaves(pass, cur, t.List[len(t.List)-1])
}

func pollCaseLeaves(pass *analysis.Pass, cur inspector.Cursor, body *ast.BlockStmt, switchLabel string) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	foundDefault := false
	for _, stmt := range body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			return false
		}
		if clause.List == nil {
			foundDefault = true
		}
		if !clauseTailLeaves(pass, cur, clause) {
			return false
		}
		if clauseHasSwitchBreak(clause.Body, switchLabel) {
			return false
		}
	}
	return foundDefault
}

func clauseTailLeaves(pass *analysis.Pass, cur inspector.Cursor, clause *ast.CaseClause) bool {
	if len(clause.Body) == 0 {
		return false
	}
	return pollLeaves(pass, cur, clause.Body[len(clause.Body)-1])
}

func clauseHasSwitchBreak(list []ast.Stmt, switchLabel string) bool {
	for _, s := range list {
		if stmtHasSwitchBreak(s, switchLabel) {
			return true
		}
	}
	return false
}

func stmtHasSwitchBreak(n ast.Node, switchLabel string) bool {
	switch t := n.(type) {
	case *ast.BranchStmt:
		return isSwitchBreak(t, switchLabel)
	case *ast.BlockStmt:
		return blockHasSwitchBreak(t.List, switchLabel)
	case *ast.IfStmt:
		return ifHasSwitchBreak(t, switchLabel)
	case *ast.LabeledStmt:
		return stmtHasSwitchBreak(t.Stmt, switchLabel)
	default:
		return false
	}
}

func blockHasSwitchBreak(list []ast.Stmt, switchLabel string) bool {
	for _, s := range list {
		if stmtHasSwitchBreak(s, switchLabel) {
			return true
		}
	}
	return false
}

func ifHasSwitchBreak(check *ast.IfStmt, switchLabel string) bool {
	if stmtHasSwitchBreak(check.Body, switchLabel) {
		return true
	}
	if check.Else == nil {
		return false
	}
	return stmtHasSwitchBreak(check.Else, switchLabel)
}

func isSwitchBreak(t *ast.BranchStmt, switchLabel string) bool {
	if t.Tok != token.BREAK {
		return false
	}
	if t.Label == nil {
		return true
	}
	return switchLabel != "" && t.Label.Name == switchLabel
}

func pollIfLeaves(pass *analysis.Pass, cur inspector.Cursor, check *ast.IfStmt) bool {
	if check.Body == nil || len(check.Body.List) == 0 {
		return false
	}
	if !pollLeaves(pass, cur, check.Body.List[len(check.Body.List)-1]) {
		return false
	}
	if check.Else == nil {
		return false
	}
	switch e := check.Else.(type) {
	case *ast.BlockStmt:
		if len(e.List) == 0 {
			return false
		}
		return pollLeaves(pass, cur, e.List[len(e.List)-1])
	case *ast.IfStmt:
		return pollIfLeaves(pass, cur, e)
	default:
		return false
	}
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
		if waits {
			return false
		}
		if sel, ok := n.(*ast.SelectStmt); ok {
			if nestedSelectWaits(pass, sel) {
				waits = true
			}
			return false
		}
		waits = waits || pollNodeWaits(pass, n)
		return !waits
	})
	return waits
}

func nestedSelectWaits(pass *analysis.Pass, sel *ast.SelectStmt) bool {
	if sel.Body == nil {
		return false
	}
	for _, stmt := range sel.Body.List {
		clause, ok := stmt.(*ast.CommClause)
		if !ok || clause.Comm != nil {
			continue
		}
		for _, s := range clause.Body {
			if defaultWaits(pass, s) {
				return true
			}
		}
		return false
	}
	return true
}

func pollNodeWaits(pass *analysis.Pass, n ast.Node) bool {
	switch t := n.(type) {
	case *ast.CallExpr:
		return isCodeCall(pass.TypesInfo, t)
	case *ast.SendStmt:
		return true
	case *ast.UnaryExpr:
		return t.Op == token.ARROW
	case *ast.SelectStmt:
		return nestedSelectWaits(pass, t)
	case *ast.RangeStmt:
		return isChanRange(pass, t)
	default:
		return false
	}
}

func isChanRange(pass *analysis.Pass, t *ast.RangeStmt) bool {
	if t.X == nil {
		return false
	}
	typ := pass.TypesInfo.TypeOf(t.X)
	if typ == nil {
		return false
	}
	_, ok := typ.Underlying().(*types.Chan)
	return ok
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
