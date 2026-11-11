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
	keys := collectKeys(pass.TypesInfo, fn)
	if len(keys) == 0 {
		return
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	facts := collectFacts(pass.TypesInfo, fn)
	for _, key := range keys {
		checkPairKey(pass, g, fn, key, facts)
	}
}

func checkPairKey(pass *analysis.Pass, g *flow.Graph, fn ast.Node, key flow.Key, facts *fnFacts) {
	info := pass.TypesInfo
	if facts.storedAddress(key) {
		return
	}
	entry := facts.entryState(fn, key)
	tr := func(n ast.Node, s state) state {
		if !facts.mentions(n, key.Root) {
			return s
		}
		return applyNode(info, key, n, s, nil)
	}
	pre, order := collectPre(g, entry, tr)
	for _, n := range order {
		if !facts.mentions(n, key.Root) {
			continue
		}
		reportPairOps(pass, n, key, pre[n])
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
	if op.method == methodUnlock && (s.kind == readKind || s.kind == freeKind) {
		pass.Reportf(op.call.Pos(), "%s is not write-locked here, so this Unlock panics: pair Lock with Unlock and RLock with RUnlock", op.recv)
	}
	if op.method == methodRUnlock && (s.kind == writeKind || s.kind == freeKind) {
		pass.Reportf(op.call.Pos(), "%s is not read-locked here, so this RUnlock panics: pair RLock with RUnlock and Lock with Unlock", op.recv)
	}
}
