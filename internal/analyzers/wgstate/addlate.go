package wgstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/types/typeutil"

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
	dones := donePositions(pass, lit)
	if len(dones) == 0 {
		return
	}
	gates := gatePositions(pass, lit)
	for _, k := range sortedDoneKeys(dones) {
		if hasAddBefore(pass, fn, g, k) {
			continue
		}
		reportLateAdds(pass, k, after, seen, dones[k], gates)
	}
}

func sortedDoneKeys(m map[flow.Key][]token.Pos) []flow.Key {
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

func reportLateAdds(pass *analysis.Pass, k flow.Key, after []ast.Stmt, seen map[token.Pos]bool, dones []token.Pos, gates map[flow.Key][]token.Pos) {
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
		if gatedAdd(pass, after, call.Pos(), dones, gates) {
			continue
		}
		seen[call.Pos()] = true
		pass.Reportf(call.Pos(), "this Add runs after the goroutine it counts has started, so Wait can return early: call %s.Add before the go statement", text)
	}
}

func gatedAdd(pass *analysis.Pass, after []ast.Stmt, addPos token.Pos, dones []token.Pos, gates map[flow.Key][]token.Pos) bool {
	for _, gk := range gatingKeys(gates, dones) {
		if releasedAfter(pass, after, addPos, gk) {
			return true
		}
	}
	return false
}

func gatingKeys(gates map[flow.Key][]token.Pos, dones []token.Pos) []flow.Key {
	out := []flow.Key{}
	for k, gpos := range gates {
		if gatesAll(gpos, dones) {
			out = append(out, k)
		}
	}
	return out
}

func gatesAll(gpos, dones []token.Pos) bool {
	if len(dones) == 0 {
		return false
	}
	for _, d := range dones {
		if !hasEarlier(gpos, d) {
			return false
		}
	}
	return true
}

func hasEarlier(gpos []token.Pos, d token.Pos) bool {
	for _, g := range gpos {
		if g < d {
			return true
		}
	}
	return false
}

func releasedAfter(pass *analysis.Pass, after []ast.Stmt, addPos token.Pos, k flow.Key) bool {
	for _, s := range after {
		if s.Pos() <= addPos {
			continue
		}
		if releasesKey(pass, s, k) {
			return true
		}
	}
	return false
}

func releasesKey(pass *analysis.Pass, s ast.Stmt, k flow.Key) bool {
	if send, ok := unwrap(s).(*ast.SendStmt); ok {
		if ck, ok := flow.KeyOf(pass.TypesInfo, send.Chan); ok && ck == k {
			return true
		}
	}
	call, ok := directCall(s)
	if !ok {
		return false
	}
	if ck, ok := closeKey(pass, call); ok && ck == k {
		return true
	}
	if uk, ok := unlockKey(pass, call); ok && uk == k {
		return true
	}
	return false
}

func closeKey(pass *analysis.Pass, call *ast.CallExpr) (flow.Key, bool) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "close" {
		return flow.Key{}, false
	}
	if _, ok := pass.TypesInfo.Uses[id].(*types.Builtin); !ok {
		return flow.Key{}, false
	}
	if len(call.Args) != 1 {
		return flow.Key{}, false
	}
	return flow.KeyOf(pass.TypesInfo, call.Args[0])
}

func unlockKey(pass *analysis.Pass, call *ast.CallExpr) (flow.Key, bool) {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return flow.Key{}, false
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Unlock", "(*sync.RWMutex).Unlock", "(*sync.RWMutex).RUnlock":
		return flow.MethodKey(pass.TypesInfo, call)
	}
	return flow.Key{}, false
}

func donePositions(pass *analysis.Pass, lit *ast.FuncLit) map[flow.Key][]token.Pos {
	info := pass.TypesInfo
	out := map[flow.Key][]token.Pos{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if k, _, ok := wgKey(info, call, "Done"); ok {
			out[k] = append(out[k], call.Pos())
		}
		return true
	})
	return out
}

func gatePositions(pass *analysis.Pass, lit *ast.FuncLit) map[flow.Key][]token.Pos {
	out := map[flow.Key][]token.Pos{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if _, ok := n.(*ast.DeferStmt); ok {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		noteRecvGate(pass, n, out)
		noteLockGate(pass, n, out)
		return true
	})
	return out
}

func noteRecvGate(pass *analysis.Pass, n ast.Node, out map[flow.Key][]token.Pos) {
	switch n := n.(type) {
	case *ast.ExprStmt:
		noteRecvExpr(pass, n.X, n.Pos(), out)
	case *ast.AssignStmt:
		for _, rhs := range n.Rhs {
			noteRecvExpr(pass, rhs, n.Pos(), out)
		}
	case *ast.ValueSpec:
		for _, v := range n.Values {
			noteRecvExpr(pass, v, n.Pos(), out)
		}
	}
}

func noteRecvExpr(pass *analysis.Pass, e ast.Expr, pos token.Pos, out map[flow.Key][]token.Pos) {
	recv, ok := ast.Unparen(e).(*ast.UnaryExpr)
	if !ok || recv.Op != token.ARROW {
		return
	}
	if k, ok := flow.KeyOf(pass.TypesInfo, recv.X); ok {
		out[k] = append(out[k], pos)
	}
}

func noteLockGate(pass *analysis.Pass, n ast.Node, out map[flow.Key][]token.Pos) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}
	if k, ok := lockGateKey(pass, call); ok {
		out[k] = append(out[k], call.Pos())
	}
}

func lockGateKey(pass *analysis.Pass, call *ast.CallExpr) (flow.Key, bool) {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return flow.Key{}, false
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock", "(*sync.RWMutex).RLock":
		return flow.MethodKey(pass.TypesInfo, call)
	}
	return flow.Key{}, false
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
