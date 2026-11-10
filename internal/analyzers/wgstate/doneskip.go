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
	Doc:      "report a return in a function literal started by go or errgroup Go that skips its direct WaitGroup Done call; a call that passes the group silences it, as does panic or testing FailNow, Fatal, or Skip, though testing calls still hang in a spawned goroutine; named functions are not checked",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runDoneskip,
}

func runDoneskip(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		for _, s := range flow.Spawns(pass, fn) {
			if s.Body == nil || s.Body.Body == nil {
				continue
			}
			if isWgGo(pass.TypesInfo, s.Site) {
				continue
			}
			checkDoneskip(pass, s.Body)
		}
	})
	return nil, nil
}

func isWgGo(info *types.Info, site ast.Node) bool {
	call, ok := site.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	return fn != nil && fn.FullName() == "(*sync.WaitGroup).Go"
}

type doneCall struct {
	key  flow.Key
	text string
	pos  token.Pos
}

func checkDoneskip(pass *analysis.Pass, lit *ast.FuncLit) {
	deferred, dones := collectDones(pass, lit)
	reportReturns(pass, lit, dones, deferred)
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

func reportReturns(pass *analysis.Pass, lit *ast.FuncLit, dones []doneCall, deferred map[flow.Key]bool) {
	eachList(lit, func(list []ast.Stmt) {
		for i, s := range list {
			ret, ok := unwrap(s).(*ast.ReturnStmt)
			if !ok {
				continue
			}
			reportSkipped(pass, lit, list[:i], dones, deferred, ret)
		}
	})
}

func reportSkipped(pass *analysis.Pass, lit *ast.FuncLit, before []ast.Stmt, dones []doneCall, deferred map[flow.Key]bool, ret *ast.ReturnStmt) {
	if failsBefore(pass.TypesInfo, before) {
		return
	}
	doneSet, releaseSet := skippedSets(pass.TypesInfo, lit, ret, before)
	seen := map[flow.Key]bool{}
	for _, d := range dones {
		if d.pos < ret.End() || deferred[d.key] || seen[d.key] {
			continue
		}
		seen[d.key] = true
		if doneSet[d.key] || releaseSet[d.key] {
			continue
		}
		pass.Reportf(ret.Pos(), "this return skips %s.Done(), so any Wait on it blocks forever: defer the Done call", d.text)
	}
}

func skippedSets(info *types.Info, lit *ast.FuncLit, ret *ast.ReturnStmt, before []ast.Stmt) (done, release map[flow.Key]bool) {
	done = directDoneSet(info, before)
	release = directReleaseSet(info, before)
	encDone, encRelease := enclosingSets(info, lit, ret)
	for k := range encDone {
		done[k] = true
	}
	for k := range encRelease {
		release[k] = true
	}
	return done, release
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

func enclosingSets(info *types.Info, lit *ast.FuncLit, ret *ast.ReturnStmt) (done, release map[flow.Key]bool) {
	done = map[flow.Key]bool{}
	release = map[flow.Key]bool{}
	if lit.Body == nil {
		return done, release
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		list, ok := stmtList(n)
		if !ok {
			return true
		}
		collectEnclosingPrefix(info, list, ret, done, release)
		return true
	})
	return done, release
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
