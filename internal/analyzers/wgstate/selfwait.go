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
	Doc:      "report a Wait on a WaitGroup declared in the function and not shared with any goroutine, closure, helper, or pointer, when constant Adds in the Wait's own block before it exceed the Done calls there; silent for parameters, fields of parameters, package variables, non-constant Adds, and any Done earlier in the function",
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
	eachList(fn, func(list []ast.Stmt) {
		for i, s := range list {
			call, ok := directCall(s)
			if !ok {
				continue
			}
			k, text, ok := wgKey(pass.TypesInfo, call, "Wait")
			if !ok {
				continue
			}
			checkWait(pass, waitSite{fn, list, i, call, k, text}, cached, local)
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

func checkWait(pass *analysis.Pass, w waitSite, cached, local map[*types.Var]bool) {
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
	adds, done, ok := straightCounts(pass, w.list, w.at, w.key)
	if !ok {
		return
	}
	if shouldReport(pass.TypesInfo, w.fn, w.key, w.call.Pos(), adds, done) {
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

func shouldReport(info *types.Info, fn ast.Node, k flow.Key, waitPos token.Pos, adds, done int64) bool {
	if adds <= done {
		return false
	}
	if countDonesBefore(info, fn, k, waitPos) > done {
		return false
	}
	return !hasNonConstAddBefore(info, fn, k, waitPos)
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

func straightCounts(pass *analysis.Pass, list []ast.Stmt, at int, k flow.Key) (adds, done int64, ok bool) {
	info := pass.TypesInfo
	for i := at - 1; i >= 0; i-- {
		if isControlStmt(list[i]) {
			break
		}
		call, ok := directCall(list[i])
		if !ok {
			continue
		}
		a, d, ok := countDelta(info, call, k)
		if !ok {
			return 0, 0, false
		}
		adds += a
		done += d
	}
	return adds, done, true
}

func isControlStmt(s ast.Stmt) bool {
	if _, ok := s.(*ast.LabeledStmt); ok {
		return true
	}
	switch unwrap(s).(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	}
	return false
}

func countDelta(info *types.Info, call *ast.CallExpr, k flow.Key) (add, done int64, ok bool) {
	if ak, _, ok := wgKey(info, call, "Add"); ok && ak == k {
		n, ok := constInt(info, call)
		if !ok {
			return 0, 0, false
		}
		return n, 0, true
	}
	if dk, _, ok := wgKey(info, call, "Done"); ok && dk == k {
		return 0, 1, true
	}
	return 0, 0, true
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

func countDonesBefore(info *types.Info, fn ast.Node, k flow.Key, waitPos token.Pos) int64 {
	body := funcBody(fn)
	if body == nil {
		return 0
	}
	var total int64
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && ast.Node(lit) != fn {
			return false
		}
		if _, ok := n.(*ast.DeferStmt); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= waitPos {
			return true
		}
		if dk, _, ok := wgKey(info, call, "Done"); ok && dk == k {
			total++
		}
		return true
	})
	return total
}

func isNonConstAdd(info *types.Info, call *ast.CallExpr, k flow.Key) bool {
	ak, _, ok := wgKey(info, call, "Add")
	if !ok || ak != k {
		return false
	}
	_, ok = constInt(info, call)
	return !ok
}

func hasNonConstAddBefore(info *types.Info, fn ast.Node, k flow.Key, waitPos token.Pos) bool {
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
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= waitPos {
			return true
		}
		if isNonConstAdd(info, call, k) {
			found = true
		}
		return !found
	})
	return found
}
