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

type gateSet struct {
	chans map[flow.Key][]token.Pos
	mutex map[flow.Key][]token.Pos
}

type lateGroup struct {
	list  []ast.Stmt
	goIdx int
	seen  map[token.Pos]bool
	gates gateSet
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
			checkGoAddlate(pass, fn, g, list, i, seen)
		}
	})
}

func checkGoAddlate(pass *analysis.Pass, fn ast.Node, g *ast.GoStmt, list []ast.Stmt, goIdx int, seen map[token.Pos]bool) {
	lit, ok := ast.Unparen(g.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return
	}
	plain, deferred := donePositions(pass, lit)
	if len(plain) == 0 && len(deferred) == 0 {
		return
	}
	gates := gatePositions(pass, lit)
	group := lateGroup{list: list, goIdx: goIdx, seen: seen, gates: gates}
	for _, k := range sortedDoneKeys(plain, deferred) {
		if hasAddBefore(pass, fn, g, k) {
			continue
		}
		reportLateAdds(pass, group, k, plain[k])
	}
}

func sortedDoneKeys(plain, deferred map[flow.Key][]token.Pos) []flow.Key {
	merged := map[flow.Key]struct{}{}
	for k := range plain {
		merged[k] = struct{}{}
	}
	for k := range deferred {
		merged[k] = struct{}{}
	}
	keys := slices.Collect(maps.Keys(merged))
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

func reportLateAdds(pass *analysis.Pass, group lateGroup, k flow.Key, plain []token.Pos) {
	after := group.list[group.goIdx+1:]
	for _, s := range after {
		call, ok := directCall(s)
		if !ok {
			continue
		}
		ak, text, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok || ak != k {
			continue
		}
		if group.seen[call.Pos()] {
			continue
		}
		if gatedAdd(pass, group, call.Pos(), plain) {
			continue
		}
		group.seen[call.Pos()] = true
		pass.Reportf(call.Pos(), "this Add runs after the goroutine it counts has started, so Wait can return early: call %s.Add before the go statement", text)
	}
}

func gatedAdd(pass *analysis.Pass, group lateGroup, addPos token.Pos, plain []token.Pos) bool {
	after := group.list[group.goIdx+1:]
	for gk, gpos := range group.gates.chans {
		if gatesAll(gpos, plain) && releasedChanAfter(pass, after, addPos, gk) {
			return true
		}
	}
	for gk, gpos := range group.gates.mutex {
		if gatesAll(gpos, plain) && heldBefore(pass, group.list, group.goIdx, addPos, gk) && releasedMutexAfter(pass, after, addPos, gk) {
			return true
		}
	}
	return false
}

func gatesAll(gpos, plain []token.Pos) bool {
	if len(plain) == 0 {
		return true
	}
	for _, d := range plain {
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

func heldBefore(pass *analysis.Pass, list []ast.Stmt, goIdx int, addPos token.Pos, k flow.Key) bool {
	for _, s := range list[:goIdx] {
		if !isLockStmt(pass, s, k) {
			continue
		}
		if noUnlockBetween(pass, list, s.Pos(), addPos, k) {
			return true
		}
	}
	return false
}

func noUnlockBetween(pass *analysis.Pass, list []ast.Stmt, lockPos, addPos token.Pos, k flow.Key) bool {
	for _, s := range list {
		if s.Pos() <= lockPos || s.Pos() > addPos {
			continue
		}
		if isUnlockStmt(pass, s, k) {
			return false
		}
	}
	return true
}

func releasedChanAfter(pass *analysis.Pass, after []ast.Stmt, addPos token.Pos, k flow.Key) bool {
	for _, s := range after {
		if s.Pos() <= addPos {
			continue
		}
		if isSendTo(pass, s, k) || isCloseOf(pass, s, k) {
			return true
		}
	}
	return false
}

func releasedMutexAfter(pass *analysis.Pass, after []ast.Stmt, addPos token.Pos, k flow.Key) bool {
	for _, s := range after {
		if s.Pos() <= addPos {
			continue
		}
		if isUnlockStmt(pass, s, k) {
			return true
		}
	}
	return false
}

func isSendTo(pass *analysis.Pass, s ast.Stmt, k flow.Key) bool {
	send, ok := unwrap(s).(*ast.SendStmt)
	if !ok {
		return false
	}
	ck, ok := flow.KeyOf(pass.TypesInfo, send.Chan)
	return ok && ck == k
}

func isCloseOf(pass *analysis.Pass, s ast.Stmt, k flow.Key) bool {
	call, ok := directCall(s)
	if !ok {
		return false
	}
	ck, ok := closeKey(pass, call)
	return ok && ck == k
}

func isLockStmt(pass *analysis.Pass, s ast.Stmt, k flow.Key) bool {
	call, ok := directCall(s)
	if !ok {
		return false
	}
	gk, ok := lockGateKey(pass, call)
	return ok && gk == k
}

func isUnlockStmt(pass *analysis.Pass, s ast.Stmt, k flow.Key) bool {
	call, ok := directCall(s)
	if !ok {
		return false
	}
	uk, ok := unlockKey(pass, call)
	return ok && uk == k
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
	case "(*sync.Mutex).Unlock", "(*sync.RWMutex).Unlock":
		return flow.MethodKey(pass.TypesInfo, call)
	}
	return flow.Key{}, false
}

func donePositions(pass *analysis.Pass, lit *ast.FuncLit) (plain, deferred map[flow.Key][]token.Pos) {
	plain = map[flow.Key][]token.Pos{}
	deferred = map[flow.Key][]token.Pos{}
	for _, s := range lit.Body.List {
		if d, ok := unwrap(s).(*ast.DeferStmt); ok {
			collectDonesIn(pass, d, deferred)
		} else {
			collectDonesIn(pass, s, plain)
		}
	}
	return plain, deferred
}

func collectDonesIn(pass *analysis.Pass, n ast.Node, out map[flow.Key][]token.Pos) {
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if k, _, ok := wgKey(pass.TypesInfo, call, "Done"); ok {
			out[k] = append(out[k], call.Pos())
		}
		return true
	})
}

func gatePositions(pass *analysis.Pass, lit *ast.FuncLit) gateSet {
	gates := gateSet{chans: map[flow.Key][]token.Pos{}, mutex: map[flow.Key][]token.Pos{}}
	for _, s := range lit.Body.List {
		noteChanGate(pass, s, gates.chans)
		noteMutexGate(pass, s, gates.mutex)
	}
	return gates
}

func noteChanGate(pass *analysis.Pass, s ast.Stmt, out map[flow.Key][]token.Pos) {
	switch n := unwrap(s).(type) {
	case *ast.ExprStmt:
		noteRecvExpr(pass, n.X, n.Pos(), out)
	case *ast.AssignStmt:
		for _, rhs := range n.Rhs {
			noteRecvExpr(pass, rhs, n.Pos(), out)
		}
	case *ast.DeclStmt:
		gen, ok := n.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				noteRecvExpr(pass, v, n.Pos(), out)
			}
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

func noteMutexGate(pass *analysis.Pass, s ast.Stmt, out map[flow.Key][]token.Pos) {
	call, ok := directCall(s)
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
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock":
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
