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
	Doc:      "report a return in a spawned function that skips its WaitGroup Done call",
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
	seen := map[flow.Key]bool{}
	for _, d := range dones {
		if d.pos < ret.End() || deferred[d.key] || seen[d.key] {
			continue
		}
		seen[d.key] = true
		if hasDoneBefore(pass.TypesInfo, lit, ret, before, d.key) {
			continue
		}
		pass.Reportf(ret.Pos(), "this return skips %s.Done(), so Wait blocks forever: defer the Done call", d.text)
	}
}

func hasDoneBefore(info *types.Info, lit *ast.FuncLit, ret *ast.ReturnStmt, before []ast.Stmt, k flow.Key) bool {
	if hasDirectDone(info, before, k) {
		return true
	}
	return hasEnclosingDone(info, lit, ret, k)
}

func hasDirectDone(info *types.Info, before []ast.Stmt, k flow.Key) bool {
	for _, s := range before {
		call, ok := directCall(s)
		if !ok {
			continue
		}
		dk, _, ok := wgKey(info, call, "Done")
		if ok && dk == k {
			return true
		}
	}
	return false
}

func hasEnclosingDone(info *types.Info, lit *ast.FuncLit, ret *ast.ReturnStmt, k flow.Key) bool {
	if lit.Body == nil {
		return false
	}
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
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
		if enclosingHasDone(info, list, ret, k) {
			found = true
		}
		return !found
	})
	return found
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

func enclosingHasDone(info *types.Info, list []ast.Stmt, ret *ast.ReturnStmt, k flow.Key) bool {
	for i, s := range list {
		if unwrap(s) == ast.Stmt(ret) {
			return false
		}
		if containsRet(s, ret) && hasDirectDone(info, list[:i], k) {
			return true
		}
	}
	return false
}

func containsRet(s ast.Stmt, ret *ast.ReturnStmt) bool {
	return s.Pos() <= ret.Pos() && ret.End() <= s.End()
}
