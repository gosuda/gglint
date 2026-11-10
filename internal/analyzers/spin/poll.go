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
	Doc:      "report for-select loops whose default case never waits; spin covers five shapes only",
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
	if isCounted(pass.TypesInfo, loop) {
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
		if posts[v] && condMentions(info, loop.Cond, v) {
			return true
		}
	}
	return false
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
		return isPollPanic(pass.TypesInfo, t.X)
	case *ast.BlockStmt:
		return pollBlockTailLeaves(pass, cur, t)
	case *ast.IfStmt:
		return pollIfLeaves(pass, cur, t)
	default:
		return false
	}
}

func pollBranchLeaves(pass *analysis.Pass, cur inspector.Cursor, t *ast.BranchStmt) bool {
	if t.Tok == token.GOTO {
		return true
	}
	if t.Tok == token.BREAK && t.Label != nil {
		return !breaksSelect(pass, cur, t.Label)
	}
	return false
}

func pollBlockTailLeaves(pass *analysis.Pass, cur inspector.Cursor, t *ast.BlockStmt) bool {
	if len(t.List) == 0 {
		return false
	}
	return pollLeaves(pass, cur, t.List[len(t.List)-1])
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

func breaksSelect(pass *analysis.Pass, cur inspector.Cursor, label *ast.Ident) bool {
	obj := pass.TypesInfo.ObjectOf(label)
	if obj == nil {
		return false
	}
	fn := enclosingFunc(cur)
	if fn == nil {
		return false
	}
	selectLabeled := false
	ast.Inspect(fn, func(n ast.Node) bool {
		labeled, ok := n.(*ast.LabeledStmt)
		if !ok {
			return true
		}
		if pass.TypesInfo.ObjectOf(labeled.Label) != obj {
			return true
		}
		_, isSelect := labeled.Stmt.(*ast.SelectStmt)
		selectLabeled = isSelect
		return false
	})
	return selectLabeled
}

func isPollPanic(info *types.Info, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.ObjectOf(id).(*types.Builtin)
	return ok && b.Name() == "panic"
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
		waits = waits || pollNodeWaits(pass, n)
		return !waits
	})
	return waits
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
		return true
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
