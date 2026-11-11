package wgstate

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

var selfwaitAnalyzer = &analysis.Analyzer{
	Name:     "selfwait",
	Doc:      "report a Wait on a WaitGroup declared in the function and not shared with any goroutine, closure, helper, or pointer, when constant Adds in the Wait's own block before it exceed the Done calls there; a branch statement or a label between the Add and the Wait ends the straight line; silent for parameters, fields of parameters, package variables, non-constant Adds, and any Done earlier in the function",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runSelfwait,
}

func runSelfwait(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkSelfwait(pass, fn)
	})
	return nil, nil
}

func checkSelfwait(pass *analysis.Pass, fn ast.Node) {
	cached := map[*types.Var]bool{}
	local := map[*types.Var]bool{}
	past := pastWaits(pass, fn)
	eachList(fn, func(list []ast.Stmt) {
		entries := buildEntries(pass, list)
		for i, s := range list {
			call, ok := directCall(s)
			if !ok {
				continue
			}
			k, text, ok := wgKey(pass.TypesInfo, call, "Wait")
			if !ok {
				continue
			}
			checkWait(pass, waitSite{fn, list, i, call, k, text}, cached, local, past, entries)
		}
	})
}

type waitSite struct {
	fn   ast.Node
	list []ast.Stmt
	at   int
	call *ast.CallExpr
	key  flow.Key
	text string
}

func isControlStmt(s ast.Stmt) bool {
	if _, ok := s.(*ast.LabeledStmt); ok {
		return true
	}
	if _, ok := s.(*ast.BranchStmt); ok {
		return true
	}
	switch unwrap(s).(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func checkWait(pass *analysis.Pass, w waitSite, cached, local map[*types.Var]bool, past *waitHistory, entries []listEntry) {
	v, ok := w.key.Root.(*types.Var)
	if !ok || !isLocalCached(pass, w.fn, v, local) {
		return
	}
	if isReachableCached(pass, w.fn, v, cached) {
		return
	}
	if w.key.Path != "" && sharesRoot(pass.TypesInfo, w.fn, v) {
		return
	}
	adds, done, ok := straightEntries(entries, w.at, w.key)
	if !ok {
		return
	}
	if shouldReport(adds, done, past.doneBefore(w.key, w.call.Pos()), past.hasNonConstBefore(w.key, w.call.Pos())) {
		pass.Reportf(w.call.Pos(), "no goroutine can call Done on %s, so this Wait blocks forever: call Done for every Add, or start the goroutines before Wait", w.text)
	}
}

func isLocalCached(pass *analysis.Pass, fn ast.Node, v *types.Var, local map[*types.Var]bool) bool {
	if known, ok := local[v]; ok {
		return known
	}
	known := flow.IsLocal(pass.TypesInfo, fn, v)
	local[v] = known
	return known
}

func isReachableCached(pass *analysis.Pass, fn ast.Node, v *types.Var, cached map[*types.Var]bool) bool {
	if known, ok := cached[v]; ok {
		return known
	}
	known := isReachable(pass, fn, v)
	cached[v] = known
	return known
}

func isReachable(pass *analysis.Pass, fn ast.Node, v *types.Var) bool {
	info := pass.TypesInfo
	if flow.Escapes(info, fn, v) {
		return true
	}
	if mentionedInLit(pass, fn, v) {
		return true
	}
	if hasMethodValue(info, fn, v) {
		return true
	}
	return hasGoDeferMention(info, fn, v)
}

func shouldReport(adds, done, doneBefore int64, hasNonConst bool) bool {
	if adds <= done {
		return false
	}
	if doneBefore > done {
		return false
	}
	return !hasNonConst
}

func mentionedInLit(pass *analysis.Pass, fn ast.Node, v *types.Var) bool {
	body := funcBody(fn)
	if body == nil {
		return false
	}
	mentioned := false
	ast.Inspect(body, func(n ast.Node) bool {
		if mentioned {
			return false
		}
		lit, ok := n.(*ast.FuncLit)
		if !ok || ast.Node(lit) == fn {
			return true
		}
		if usesVar(pass.TypesInfo, lit, v) {
			mentioned = true
		}
		return false
	})
	return mentioned
}

func usesVar(info *types.Info, lit *ast.FuncLit, v *types.Var) bool {
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if info.Uses[id] == v || info.Defs[id] == v {
			found = true
		}
		return !found
	})
	return found
}

type listEntry struct {
	ctrl    bool
	addK    flow.Key
	addN    int64
	addBad  bool
	hasAdd  bool
	doneK   flow.Key
	hasDone bool
}

func buildEntries(pass *analysis.Pass, list []ast.Stmt) []listEntry {
	out := make([]listEntry, len(list))
	for i, s := range list {
		if isControlStmt(s) {
			out[i].ctrl = true
			continue
		}
		call, ok := directCall(s)
		if !ok {
			continue
		}
		if k, _, ok := wgKey(pass.TypesInfo, call, "Add"); ok {
			out[i].hasAdd = true
			out[i].addK = k
			if n, ok := constInt(pass.TypesInfo, call); ok {
				out[i].addN = n
			} else {
				out[i].addBad = true
			}
			continue
		}
		if k, _, ok := wgKey(pass.TypesInfo, call, "Done"); ok {
			out[i].hasDone = true
			out[i].doneK = k
		}
	}
	return out
}

func straightEntries(entries []listEntry, at int, k flow.Key) (adds, done int64, ok bool) {
	for i := at - 1; i >= 0; i-- {
		e := entries[i]
		if e.ctrl {
			break
		}
		if e.hasAdd && e.addK == k {
			if e.addBad {
				return 0, 0, false
			}
			adds += e.addN
		}
		if e.hasDone && e.doneK == k {
			done++
		}
	}
	return adds, done, true
}

func constInt(info *types.Info, call *ast.CallExpr) (int64, bool) {
	if len(call.Args) != 1 {
		return 0, false
	}
	v := info.Types[call.Args[0]].Value
	if v == nil || v.Kind() != constant.Int {
		return 0, false
	}
	n, exact := constant.Int64Val(v)
	if !exact {
		return 0, false
	}
	return n, true
}

func rootIs(info *types.Info, e ast.Expr, v *types.Var) bool {
	k, ok := flow.KeyOf(info, e)
	return ok && k.Root == v
}

func isValueMethod(info *types.Info, e ast.Expr, v *types.Var) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return false
	}
	return rootIs(info, sel.X, v)
}

func hasMethodValue(info *types.Info, fn ast.Node, v *types.Var) bool {
	body := funcBody(fn)
	if body == nil {
		return false
	}
	funs := map[*ast.SelectorExpr]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
			funs[sel] = true
		}
		return true
	})
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || funs[sel] {
			return true
		}
		if isValueMethod(info, sel, v) {
			found = true
		}
		return !found
	})
	return found
}

func callMentions(info *types.Info, call *ast.CallExpr, v *types.Var) bool {
	found := false
	ast.Inspect(call, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if info.Uses[id] == v || info.Defs[id] == v {
			found = true
		}
		return !found
	})
	return found
}

func hasGoDeferMention(info *types.Info, fn ast.Node, v *types.Var) bool {
	body := funcBody(fn)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && ast.Node(lit) != fn {
			return false
		}
		if g, ok := n.(*ast.GoStmt); ok && callMentions(info, g.Call, v) {
			found = true
		}
		return !found
	})
	return found
}

func sharesViaCall(info *types.Info, call *ast.CallExpr, v *types.Var) bool {
	for _, a := range call.Args {
		if rootIs(info, a, v) {
			return true
		}
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || !rootIs(info, sel.X, v) {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return true
	}
	switch fn.FullName() {
	case "(*sync.WaitGroup).Add", "(*sync.WaitGroup).Done", "(*sync.WaitGroup).Wait", "(*sync.WaitGroup).Go":
		return false
	}
	return true
}

func notesSharing(info *types.Info, n ast.Node, v *types.Var) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.AND && rootIs(info, n.X, v)
	case *ast.CallExpr:
		return sharesViaCall(info, n, v)
	}
	return false
}

func sharesRoot(info *types.Info, fn ast.Node, v *types.Var) bool {
	body := funcBody(fn)
	if body == nil {
		return false
	}
	shared := false
	ast.Inspect(body, func(n ast.Node) bool {
		if shared {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && ast.Node(lit) != fn {
			return false
		}
		if notesSharing(info, n, v) {
			shared = true
		}
		return !shared
	})
	return shared
}

type waitHistory struct {
	donePos  map[flow.Key][]token.Pos
	nonConst map[flow.Key]token.Pos
}

func pastWaits(pass *analysis.Pass, fn ast.Node) *waitHistory {
	past := &waitHistory{donePos: map[flow.Key][]token.Pos{}, nonConst: map[flow.Key]token.Pos{}}
	body := funcBody(fn)
	if body == nil {
		return past
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && ast.Node(lit) != fn {
			return false
		}
		if _, ok := n.(*ast.DeferStmt); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			past.noteDone(pass.TypesInfo, call)
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && ast.Node(lit) != fn {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			past.noteNonConst(pass.TypesInfo, call)
		}
		return true
	})
	return past
}

func (p *waitHistory) noteDone(info *types.Info, call *ast.CallExpr) {
	if k, _, ok := wgKey(info, call, "Done"); ok {
		p.donePos[k] = append(p.donePos[k], call.Pos())
	}
}

func (p *waitHistory) noteNonConst(info *types.Info, call *ast.CallExpr) {
	k, _, ok := wgKey(info, call, "Add")
	if !ok {
		return
	}
	if _, ok := constInt(info, call); ok {
		return
	}
	if q, dup := p.nonConst[k]; !dup || call.Pos() < q {
		p.nonConst[k] = call.Pos()
	}
}

func (p *waitHistory) doneBefore(k flow.Key, waitPos token.Pos) int64 {
	var total int64
	for _, q := range p.donePos[k] {
		if q < waitPos {
			total++
		}
	}
	return total
}

func (p *waitHistory) hasNonConstBefore(k flow.Key, waitPos token.Pos) bool {
	q, ok := p.nonConst[k]
	return ok && q < waitPos
}
