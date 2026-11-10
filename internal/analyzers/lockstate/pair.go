package lockstate

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

func runPair(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkPair(pass, cur.Node())
	}
	return nil, nil
}

func checkPair(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	for _, key := range collectKeys(pass.TypesInfo, fn) {
		checkPairKey(pass, g, fn, key)
	}
}

func checkPairKey(pass *analysis.Pass, g *flow.Graph, fn ast.Node, key flow.Key) {
	info := pass.TypesInfo
	entry := entryState(info, fn, key)
	tr := func(n ast.Node, s state) state {
		return applyNode(info, key, n, s)
	}
	pre := collectPre(g, entry, tr)
	for n, s := range pre {
		reportPairOps(pass, n, key, s)
	}
}

func reportPairOps(pass *analysis.Pass, n ast.Node, key flow.Key, s state) {
	if _, ok := n.(*ast.DeferStmt); ok {
		return
	}
	for _, op := range opsInNode(pass.TypesInfo, n, key) {
		reportPairOp(pass, op, s)
	}
}

func reportPairOp(pass *analysis.Pass, op opCall, s state) {
	if op.method == methodUnlock && (s == readState || s == freeState) {
		pass.Reportf(op.call.Pos(), "%s is not write-locked here, so this Unlock panics: pair Lock with Unlock and RLock with RUnlock", op.recv)
	}
	if op.method == methodRUnlock && (s == writeState || s == freeState) {
		pass.Reportf(op.call.Pos(), "%s is not read-locked here, so this RUnlock panics: pair RLock with RUnlock and Lock with Unlock", op.recv)
	}
}
