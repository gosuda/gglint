package wgstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
)

var addlateAnalyzer = &analysis.Analyzer{
	Name:     "addlate",
	Doc:      "report a WaitGroup Add that runs after the counted goroutine has started",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runAddlate,
}

func runAddlate(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkAddlate(pass, fn)
	})
	return nil, nil
}

func checkAddlate(pass *analysis.Pass, fn ast.Node) {
	eachList(fn, func(list []ast.Stmt) {
		seen := map[token.Pos]bool{}
		for i, s := range list {
			g, ok := unwrap(s).(*ast.GoStmt)
			if !ok {
				continue
			}
			checkGoAddlate(pass, fn, g, list[i+1:], seen)
		}
	})
}

func checkGoAddlate(pass *analysis.Pass, fn ast.Node, g *ast.GoStmt, after []ast.Stmt, seen map[token.Pos]bool) {
	lit, ok := ast.Unparen(g.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return
	}
	for _, k := range sortedDoneKeys(doneKeys(pass, lit)) {
		if hasAddBefore(pass, fn, g, k) {
			continue
		}
		reportLateAdds(pass, k, after, seen)
	}
}

func sortedDoneKeys(m map[flow.Key]bool) []flow.Key {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b flow.Key) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(keyName(a), keyName(b)))
	})
	return keys
}

func keyName(k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	return k.Root.Name()
}

func reportLateAdds(pass *analysis.Pass, k flow.Key, after []ast.Stmt, seen map[token.Pos]bool) {
	info := pass.TypesInfo
	for _, s := range after {
		call, ok := directCall(s)
		if !ok {
			continue
		}
		ak, text, ok := wgKey(info, call, "Add")
		if !ok || ak != k {
			continue
		}
		if seen[call.Pos()] {
			continue
		}
		seen[call.Pos()] = true
		pass.Reportf(call.Pos(), "this Add runs after the goroutine it counts has started, so Wait can return early: call %s.Add before the go statement", text)
	}
}

func doneKeys(pass *analysis.Pass, lit *ast.FuncLit) map[flow.Key]bool {
	info := pass.TypesInfo
	out := map[flow.Key]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if k, _, ok := wgKey(info, call, "Done"); ok {
			out[k] = true
		}
		return true
	})
	return out
}

func hasAddBefore(pass *analysis.Pass, fn ast.Node, g *ast.GoStmt, k flow.Key) bool {
	info := pass.TypesInfo
	found := false
	ast.Inspect(funcBody(fn), func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= g.Pos() {
			return true
		}
		ak, _, ok := wgKey(info, call, "Add")
		if ok && ak == k {
			found = true
		}
		return true
	})
	return found
}
