package wgstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

var doneskipAnalyzer = &analysis.Analyzer{
	Name:     "doneskip",
	Doc:      "report a return in a function literal started by go, errgroup Go or TryGo, or WaitGroup Go that skips its direct WaitGroup Done call; a return inside a loop that also calls Done on a path that continues the loop is not reported; a call that passes the group silences it, as does panic or testing FailNow, Fatal, or Skip, though testing calls still hang in a spawned goroutine; named functions are not checked",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runDoneskip,
}

func runDoneskip(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		for _, s := range flow.Spawns(pass, fn) {
			if s.Body == nil || s.Body.Body == nil {
				continue
			}
			checkDoneskip(pass, s.Body)
		}
	})
	return nil, nil
}

type doneCall struct {
	key  flow.Key
	text string
	pos  token.Pos
}

func checkDoneskip(pass *analysis.Pass, lit *ast.FuncLit) {
	deferred, dones := collectDones(pass, lit)
	ctx := newSkipCtx(pass, lit, dones, deferred)
	reportReturns(ctx)
}

func collectDones(pass *analysis.Pass, lit *ast.FuncLit) (map[flow.Key]bool, []doneCall) {
	info := pass.TypesInfo
	deferred := map[flow.Key]bool{}
	var dones []doneCall
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		noteDoneNode(info, deferred, &dones, n)
		return true
	})
	return deferred, dones
}

func noteDoneNode(info *types.Info, deferred map[flow.Key]bool, dones *[]doneCall, n ast.Node) {
	switch n := n.(type) {
	case *ast.DeferStmt:
		if k, _, ok := wgKey(info, n.Call, "Done"); ok {
			deferred[k] = true
		}
	case *ast.ExprStmt:
		noteDoneExpr(info, dones, n)
	}
}

func noteDoneExpr(info *types.Info, dones *[]doneCall, n *ast.ExprStmt) {
	call, ok := ast.Unparen(n.X).(*ast.CallExpr)
	if !ok {
		return
	}
	if k, _, ok := wgKey(info, call, "Done"); ok {
		*dones = append(*dones, doneCall{k, recvText(call), n.Pos()})
	}
}

type skipCtx struct {
	pass         *analysis.Pass
	info         *types.Info
	lit          *ast.FuncLit
	dones        []doneCall
	deferred     map[flow.Key]bool
	doneKeys     []flow.Key
	firstDone    map[flow.Key]token.Pos
	firstRelease map[flow.Key]token.Pos
	topReady     bool
}

func newSkipCtx(pass *analysis.Pass, lit *ast.FuncLit, dones []doneCall, deferred map[flow.Key]bool) *skipCtx {
	info := pass.TypesInfo
	ctx := &skipCtx{pass: pass, info: info, lit: lit, dones: dones, deferred: deferred}
	seen := map[flow.Key]bool{}
	for _, d := range dones {
		if !seen[d.key] {
			seen[d.key] = true
			ctx.doneKeys = append(ctx.doneKeys, d.key)
		}
	}
	return ctx
}

func (ctx *skipCtx) ensureTop() {
	if ctx.topReady {
		return
	}
	ctx.topReady = true
	ctx.firstDone = map[flow.Key]token.Pos{}
	ctx.firstRelease = map[flow.Key]token.Pos{}
	if ctx.lit.Body == nil {
		return
	}
	for _, s := range ctx.lit.Body.List {
		for _, k := range stmtDoneKeys(ctx.info, s) {
			if _, ok := ctx.firstDone[k]; !ok {
				ctx.firstDone[k] = s.Pos()
			}
		}
		for _, k := range stmtReleaseKeys(ctx.info, s) {
			if _, ok := ctx.firstRelease[k]; !ok {
				ctx.firstRelease[k] = s.Pos()
			}
		}
	}
}

func reportReturns(ctx *skipCtx) {
	eachList(ctx.lit, func(list []ast.Stmt) {
		for i, s := range list {
			ret, ok := unwrap(s).(*ast.ReturnStmt)
			if !ok {
				continue
			}
			reportSkipped(ctx, list[:i], ret)
		}
	})
}

func reportSkipped(ctx *skipCtx, before []ast.Stmt, ret *ast.ReturnStmt) {
	if failsBefore(ctx.info, before) {
		return
	}
	doneSet, releaseSet := skippedSets(ctx, ret, before)
	seen := map[flow.Key]bool{}
	for _, d := range ctx.dones {
		if d.pos < ret.End() || ctx.deferred[d.key] || seen[d.key] {
			continue
		}
		seen[d.key] = true
		if doneSet[d.key] || releaseSet[d.key] {
			continue
		}
		if loopContinuesDone(ctx, ret, d.key) {
			continue
		}
		ctx.pass.Reportf(ret.Pos(), "this return skips %s.Done(), so any Wait on it blocks forever: defer the Done call", d.text)
	}
}

func loopContinuesDone(ctx *skipCtx, ret *ast.ReturnStmt, k flow.Key) bool {
	loop := enclosingLoop(ctx.lit, ret)
	if loop == nil {
		return false
	}
	return loopHasContinuingDone(ctx.info, loop, k)
}

func enclosingLoop(lit *ast.FuncLit, ret *ast.ReturnStmt) ast.Node {
	var best ast.Node
	var bestPos token.Pos
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			if n.Pos() <= ret.Pos() && ret.End() <= n.End() && n.Pos() > bestPos {
				best = n
				bestPos = n.Pos()
			}
		case *ast.FuncLit:
			return false
		}
		return true
	})
	return best
}

func loopHasContinuingDone(info *types.Info, loop ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(loop, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		list, ok := stmtList(n)
		if !ok {
			return true
		}
		if listContinuesDone(info, list, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func listContinuesDone(info *types.Info, list []ast.Stmt, k flow.Key) bool {
	for i, s := range list {
		if !stmtHasDone(info, s, k) {
			continue
		}
		if !followsExit(info, list[i+1:]) {
			return true
		}
	}
	return false
}

func stmtHasDone(info *types.Info, s ast.Stmt, k flow.Key) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ak, _, ok := wgKey(info, call, "Done"); ok && ak == k {
			found = true
			return false
		}
		return true
	})
	return found
}

func followsExit(info *types.Info, rest []ast.Stmt) bool {
	for _, s := range rest {
		u := unwrap(s)
		if _, ok := u.(*ast.ReturnStmt); ok {
			return true
		}
		if b, ok := u.(*ast.BranchStmt); ok && (b.Tok == token.BREAK || b.Tok == token.GOTO) {
			return true
		}
		if call, ok := directCall(s); ok && isEndingCall(info, call) {
			return true
		}
	}
	return false
}

func isEndingCall(info *types.Info, call *ast.CallExpr) bool {
	return isPanicCall(info, call) || isTestFail(info, call) || isExitCall(info, call)
}

func failsBefore(info *types.Info, before []ast.Stmt) bool {
	for _, s := range before {
		call, ok := directCall(s)
		if !ok {
			continue
		}
		if isPanicCall(info, call) {
			return true
		}
		if isTestFail(info, call) {
			return true
		}
		if isExitCall(info, call) {
			return true
		}
	}
	return false
}

func isPanicCall(info *types.Info, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "panic" {
		return false
	}
	return info.ObjectOf(id) == types.Universe.Lookup("panic")
}

func isTestFail(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return false
	}
	pkg := fn.Pkg()
	if pkg == nil || pkg.Path() != "testing" {
		return false
	}
	switch fn.Name() {
	case "FailNow", "Fatal", "Fatalf", "Skip", "SkipNow", "Skipf":
		return true
	}
	return false
}

func isExitCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() + "." + fn.Name() {
	case "os.Exit", "log.Fatal", "log.Fatalf", "log.Fatalln":
		return true
	}
	return false
}

func directDoneSet(info *types.Info, before []ast.Stmt) map[flow.Key]bool {
	out := map[flow.Key]bool{}
	for _, s := range before {
		for _, k := range stmtDoneKeys(info, s) {
			out[k] = true
		}
	}
	return out
}

func stmtDoneKeys(info *types.Info, s ast.Stmt) []flow.Key {
	if b, ok := unwrap(s).(*ast.BlockStmt); ok {
		var out []flow.Key
		for _, inner := range b.List {
			out = append(out, stmtDoneKeys(info, inner)...)
		}
		return out
	}
	call, ok := directCall(s)
	if !ok {
		return nil
	}
	if k, _, ok := wgKey(info, call, "Done"); ok {
		return []flow.Key{k}
	}
	return nil
}

func directReleaseSet(info *types.Info, before []ast.Stmt) map[flow.Key]bool {
	out := map[flow.Key]bool{}
	for _, s := range before {
		for _, k := range stmtReleaseKeys(info, s) {
			out[k] = true
		}
	}
	return out
}

func stmtReleaseKeys(info *types.Info, s ast.Stmt) []flow.Key {
	if b, ok := unwrap(s).(*ast.BlockStmt); ok {
		var out []flow.Key
		for _, inner := range b.List {
			out = append(out, stmtReleaseKeys(info, inner)...)
		}
		return out
	}
	if d, ok := unwrap(s).(*ast.DeferStmt); ok {
		return callArgKeys(info, d.Call)
	}
	call, ok := directCall(s)
	if !ok {
		return nil
	}
	return callArgKeys(info, call)
}

func callArgKeys(info *types.Info, call *ast.CallExpr) []flow.Key {
	var out []flow.Key
	for _, a := range call.Args {
		if k, ok := flow.KeyOf(info, a); ok {
			out = append(out, k)
		}
	}
	return out
}

func skippedSets(ctx *skipCtx, ret *ast.ReturnStmt, before []ast.Stmt) (done, release map[flow.Key]bool) {
	done = directDoneSet(ctx.info, before)
	release = directReleaseSet(ctx.info, before)
	encDone, encRelease := enclosingSets(ctx, ret)
	for k := range encDone {
		done[k] = true
	}
	for k := range encRelease {
		release[k] = true
	}
	return done, release
}

func enclosingSets(ctx *skipCtx, ret *ast.ReturnStmt) (done, release map[flow.Key]bool) {
	done = map[flow.Key]bool{}
	release = map[flow.Key]bool{}
	lit := ctx.lit
	if lit.Body == nil {
		return done, release
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if !descendEnclosing(lit, n, ret) {
			return false
		}
		handleEnclosing(ctx, n, ret, done, release)
		return true
	})
	return done, release
}

func descendEnclosing(lit *ast.FuncLit, n ast.Node, ret *ast.ReturnStmt) bool {
	if n == nil {
		return true
	}
	if n != ast.Node(lit.Body) && (n.End() <= ret.Pos() || n.Pos() >= ret.End()) {
		return false
	}
	if _, ok := n.(*ast.FuncLit); ok {
		return false
	}
	return true
}

func handleEnclosing(ctx *skipCtx, n ast.Node, ret *ast.ReturnStmt, done, release map[flow.Key]bool) {
	list, ok := stmtList(n)
	if !ok {
		return
	}
	if b, ok := n.(*ast.BlockStmt); ok && b == ctx.lit.Body {
		collectTopPrefix(ctx, list, ret, done, release)
		return
	}
	collectEnclosingPrefix(ctx.info, list, ret, done, release)
}

func collectTopPrefix(ctx *skipCtx, list []ast.Stmt, ret *ast.ReturnStmt, done, release map[flow.Key]bool) {
	ctx.ensureTop()
	for _, s := range list {
		if unwrap(s) == ast.Stmt(ret) {
			return
		}
		if !containsRet(s, ret) {
			continue
		}
		ctx.markTopPrefix(s.Pos(), done, release)
		return
	}
}

func (ctx *skipCtx) markTopPrefix(at token.Pos, done, release map[flow.Key]bool) {
	for _, k := range ctx.doneKeys {
		if p, ok := ctx.firstDone[k]; ok && p < at {
			done[k] = true
		}
		if p, ok := ctx.firstRelease[k]; ok && p < at {
			release[k] = true
		}
	}
}

func collectEnclosingPrefix(info *types.Info, list []ast.Stmt, ret *ast.ReturnStmt, done, release map[flow.Key]bool) {
	for i, s := range list {
		if unwrap(s) == ast.Stmt(ret) {
			return
		}
		if !containsRet(s, ret) {
			continue
		}
		for _, k := range prefixDoneKeys(info, list[:i]) {
			done[k] = true
		}
		for _, k := range prefixReleaseKeys(info, list[:i]) {
			release[k] = true
		}
	}
}

func prefixDoneKeys(info *types.Info, prefix []ast.Stmt) []flow.Key {
	var out []flow.Key
	for _, s := range prefix {
		out = append(out, stmtDoneKeys(info, s)...)
	}
	return out
}

func prefixReleaseKeys(info *types.Info, prefix []ast.Stmt) []flow.Key {
	var out []flow.Key
	for _, s := range prefix {
		out = append(out, stmtReleaseKeys(info, s)...)
	}
	return out
}

func stmtList(n ast.Node) ([]ast.Stmt, bool) {
	switch n := n.(type) {
	case *ast.BlockStmt:
		return n.List, true
	case *ast.CaseClause:
		return n.Body, true
	case *ast.CommClause:
		return n.Body, true
	}
	return nil, false
}

func containsRet(s ast.Stmt, ret *ast.ReturnStmt) bool {
	return s.Pos() <= ret.Pos() && ret.End() <= s.End()
}
