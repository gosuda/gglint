package lockstate

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

func runUpgrade(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkUpgrade(pass, cur.Node())
	}
	return nil, nil
}

func checkUpgrade(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	for _, key := range collectKeys(pass.TypesInfo, fn) {
		checkUpgradeKey(pass, g, fn, key)
	}
}

func checkUpgradeKey(pass *analysis.Pass, g *flow.Graph, fn ast.Node, key flow.Key) {
	info := pass.TypesInfo
	if storedAddress(info, fn, key) {
		return
	}
	entry := entryState(info, fn, key)
	tr := func(n ast.Node, s state) state {
		return applyNode(info, key, n, s)
	}
	pre, order := collectPre(g, entry, tr)
	for _, n := range order {
		reportUpgradeOps(pass, n, key, pre[n])
	}
}

func reportUpgradeOps(pass *analysis.Pass, n ast.Node, key flow.Key, s state) {
	if _, ok := n.(*ast.DeferStmt); ok {
		return
	}
	if s.kind != readKind {
		return
	}
	for _, op := range opsInNode(pass.TypesInfo, n, key) {
		if op.method == methodLock {
			pass.Reportf(op.call.Pos(), "%s is read-locked by this goroutine, so this Lock waits for itself: RUnlock first, or take Lock from the start", op.recv)
		}
	}
}
