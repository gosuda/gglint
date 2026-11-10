package wgstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

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
			if s.Body != nil && s.Body.Body != nil {
				checkDoneskip(pass, s.Body)
			}
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
	info := pass.TypesInfo
	deferred := map[flow.Key]bool{}
	var dones []doneCall
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.DeferStmt:
			if k, _, ok := wgKey(info, n.Call, "Done"); ok {
				deferred[k] = true
			}
		case *ast.ExprStmt:
			if call, ok := ast.Unparen(n.X).(*ast.CallExpr); ok {
				if k, _, ok := wgKey(info, call, "Done"); ok {
					dones = append(dones, doneCall{k, recvText(call), n.Pos()})
				}
			}
		}
		return true
	})
	eachList(lit, func(list []ast.Stmt) {
		for i, s := range list {
			ret, ok := s.(*ast.ReturnStmt)
			if !ok {
				continue
			}
			reportSkipped(pass, list[:i], dones, deferred, ret)
		}
	})
}

func reportSkipped(pass *analysis.Pass, before []ast.Stmt, dones []doneCall, deferred map[flow.Key]bool, ret *ast.ReturnStmt) {
	seen := map[flow.Key]bool{}
	for _, d := range dones {
		if d.pos < ret.End() || deferred[d.key] || seen[d.key] {
			continue
		}
		seen[d.key] = true
		if hasDoneBefore(pass.TypesInfo, before, d.key) {
			continue
		}
		pass.Reportf(ret.Pos(), "this return skips %s.Done(), so Wait blocks forever: defer the Done call", d.text)
	}
}
func hasDoneBefore(info *types.Info, before []ast.Stmt, k flow.Key) bool {
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
