package wgstate

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
)

var selfwaitAnalyzer = &analysis.Analyzer{
	Name:     "selfwait",
	Doc:      "report a WaitGroup Wait that no goroutine can release",
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
			checkWait(pass, waitSite{fn, list, i, call, k, text})
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

func checkWait(pass *analysis.Pass, w waitSite) {
	v, ok := w.key.Root.(*types.Var)
	if !ok || !flow.IsLocal(pass.TypesInfo, w.fn, v) {
		return
	}
	if flow.Escapes(pass.TypesInfo, w.fn, v) {
		return
	}
	if mentionedInLit(pass, w.fn, v) {
		return
	}
	adds, done, ok := straightCounts(pass, w.fn, w.list, w.at, w.key)
	if !ok {
		return
	}
	if adds > done {
		pass.Reportf(w.call.Pos(), "no goroutine can call Done on %s, so this Wait blocks forever: call Done for every Add, or start the goroutines before Wait", w.text)
	}
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

func straightCounts(pass *analysis.Pass, fn ast.Node, list []ast.Stmt, at int, k flow.Key) (int64, int64, bool) {
	info := pass.TypesInfo
	var adds int64
	var done int64
	wait := list[at]
	for _, s := range list[:at] {
		call, ok := directCall(s)
		if !ok || !flow.StraightLine(fn, s, wait) {
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

func countDelta(info *types.Info, call *ast.CallExpr, k flow.Key) (int64, int64, bool) {
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
