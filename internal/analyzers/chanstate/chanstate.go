package chanstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the seven channel lifecycle checks.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		mk("nilop", "reports channel operations on a local channel declared with var and never made", runNilop),
		mk("doubleclose", "reports a close of a channel that is already closed on every path", runDoubleclose),
		mk("sendafterclose", "reports a send to a channel that is closed on every path", runSendafterclose),
		mk("selfdeadlock", "reports an unbuffered channel whose send and receive are both in this goroutine", runSelfdeadlock),
		mk("rangeunclosed", "reports a range over a local channel that no code closes", runRangeunclosed),
		mk("orphansend", "reports a spawned send that blocks forever when the parent returns through its other select case", runOrphansend),
		mk("firstresult", "reports more goroutine sends on a channel than the receives and capacity can drain; silent when the loop body receives", runFirstresult),
	}
}

func mk(name, doc string, run func(*analysis.Pass) (any, error)) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     name,
		Doc:      doc,
		Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
		Run:      run,
	}
}

const (
	msgNilop          = "%s is declared and never made, so this operation blocks forever (close panics): make the channel first; a nil channel is only useful as a disabled select case"
	msgDoubleclose    = "%s is already closed on every path to this close, which panics: close it once"
	msgSendafterclose = "%s is closed on every path to this send, which panics: send before the close"
	msgSelfdeadlock   = "%s is unbuffered and both ends are in this goroutine, so this operation deadlocks: give it capacity 1, or hand one end to another goroutine"
	msgRangeunclosed  = "no code closes %s, so this range never ends: close it when the senders finish"
	msgOrphansend     = "when the parent returns through its other select case, this send on %s blocks forever and the goroutine leaks: give %s capacity 1, or select on the same cancellation in the sender"
	msgFirstresult    = "%d goroutines send on %s and %d values are received with capacity %d, so the others block forever: size the buffer to the fan-out, or receive in a loop"
)

func runNilop(pass *analysis.Pass) (any, error) {
	each(pass, func(pass *analysis.Pass, fn ast.Node, g *flow.Graph) {
		for _, k := range nilLocals(pass.TypesInfo, fn) {
			machineReport(pass, fn, g, k, reportNilop)
			nilRangeReport(pass, fn, k)
		}
	})
	return nil, nil
}

func nilRangeReport(pass *analysis.Pass, fn ast.Node, k flow.Key) {
	info := pass.TypesInfo
	for _, r := range chanRanges(info, fn) {
		rk, ok := flow.KeyOf(info, r.X)
		if !ok || rk != k {
			continue
		}
		pass.Reportf(r.X.Pos(), msgNilop, chanName(k))
	}
}

func reportNilop(pass *analysis.Pass, op chanOp, s chanState) {
	if op.Kind&(opSend|opRecv|opClose) == 0 || op.InSelect || s != stNil {
		return
	}
	pass.Reportf(op.Pos, msgNilop, chanName(op.Key))
}

func runDoubleclose(pass *analysis.Pass) (any, error) {
	each(pass, func(pass *analysis.Pass, fn ast.Node, g *flow.Graph) {
		for _, k := range chanLocals(pass.TypesInfo, fn) {
			machineReport(pass, fn, g, k, reportDoubleclose)
		}
	})
	return nil, nil
}

func reportDoubleclose(pass *analysis.Pass, op chanOp, s chanState) {
	if op.Kind == opClose && s == stClosed {
		pass.Reportf(op.Pos, msgDoubleclose, chanName(op.Key))
	}
}

func runSendafterclose(pass *analysis.Pass) (any, error) {
	each(pass, func(pass *analysis.Pass, fn ast.Node, g *flow.Graph) {
		for _, k := range chanLocals(pass.TypesInfo, fn) {
			machineReport(pass, fn, g, k, reportSendafterclose)
		}
	})
	return nil, nil
}

func reportSendafterclose(pass *analysis.Pass, op chanOp, s chanState) {
	if op.Kind == opSend && !op.InSelect && s == stClosed {
		pass.Reportf(op.Pos, msgSendafterclose, chanName(op.Key))
	}
}

func runSelfdeadlock(pass *analysis.Pass) (any, error) {
	each(pass, selfdeadlockCheck)
	return nil, nil
}

func selfdeadlockCheck(pass *analysis.Pass, fn ast.Node, g *flow.Graph) {
	info := pass.TypesInfo
	for _, k := range chanLocals(info, fn) {
		if !unbuffered(info, fn, k) || literalMentions(info, fn, k) || channelCopied(info, fn, k) {
			continue
		}
		if first, ok := deadPair(g, chanOps(info, fn, k, opSend|opRecv)); ok {
			pass.Reportf(first.Pos, msgSelfdeadlock, chanName(k))
		}
	}
}

func runRangeunclosed(pass *analysis.Pass) (any, error) {
	each(pass, rangeunclosedCheck)
	return nil, nil
}

func rangeunclosedCheck(pass *analysis.Pass, fn ast.Node, _ *flow.Graph) {
	info := pass.TypesInfo
	for _, r := range chanRanges(info, fn) {
		k, ok := rangeUnclosedKey(info, fn, r)
		if !ok {
			continue
		}
		pass.Reportf(r.X.Pos(), msgRangeunclosed, chanName(k))
	}
}

func rangeUnclosedKey(info *types.Info, fn ast.Node, r *ast.RangeStmt) (flow.Key, bool) {
	k, ok := rangeKeyDecl(info, fn, r)
	if !ok {
		return flow.Key{}, false
	}
	if rangeKeyClosed(info, fn, r, k) {
		return flow.Key{}, false
	}
	return k, true
}

func rangeKeyDecl(info *types.Info, fn ast.Node, r *ast.RangeStmt) (flow.Key, bool) {
	if _, ok := ast.Unparen(r.X).(*ast.Ident); !ok {
		return flow.Key{}, false
	}
	k, ok := flow.KeyOf(info, r.X)
	if !ok || k.Path != "" || !localKey(info, fn, k) {
		return flow.Key{}, false
	}
	if _, ok := makeDef(info, fn, k); !ok {
		return flow.Key{}, false
	}
	if channelCopied(info, fn, k) || reassigned(info, fn, k) {
		return flow.Key{}, false
	}
	return k, true
}

func rangeKeyClosed(info *types.Info, fn ast.Node, r *ast.RangeStmt, k flow.Key) bool {
	if closesAnywhere(info, fn, k) {
		return true
	}
	if handsOn(info, fn, k) {
		return true
	}
	return loopExits(fn, r)
}

func runOrphansend(pass *analysis.Pass) (any, error) {
	each(pass, orphansendCheck)
	return nil, nil
}

func orphansendCheck(pass *analysis.Pass, fn ast.Node, _ *flow.Graph) {
	info := pass.TypesInfo
	for _, sp := range flow.Spawns(pass, fn) {
		for _, op := range loneSends(info, sp.Body) {
			if !unbuffered(info, fn, op.Key) {
				continue
			}
			if handsOn(info, fn, op.Key) || spawnDrains(info, fn, op.Key) {
				continue
			}
			sel, recv, ok := loneSelectRecv(info, fn, op.Key)
			if !ok || !otherReturns(sel, recv.Expr) {
				continue
			}
			pass.Reportf(op.Pos, msgOrphansend, chanName(op.Key), chanName(op.Key))
		}
	}
}

func runFirstresult(pass *analysis.Pass) (any, error) {
	each(pass, firstresultCheck)
	return nil, nil
}

func firstresultCheck(pass *analysis.Pass, fn ast.Node, _ *flow.Graph) {
	info := pass.TypesInfo
	for _, st := range countLoops(fn) {
		n, ok := loopTrip(info, st)
		if !ok {
			continue
		}
		for _, goStmt := range directGos(loopBody(st)) {
			firstresultGo(pass, info, fn, st, goStmt, n)
		}
	}
}

func firstresultGo(pass *analysis.Pass, info *types.Info, fn ast.Node, loop ast.Stmt, goStmt *ast.GoStmt, n int64) {
	lit, ok := ast.Unparen(goStmt.Call.Fun).(*ast.FuncLit)
	if !ok {
		return
	}
	op, ok := loneSend(info, lit)
	if !ok {
		return
	}
	kcap, ok := madeCap(info, fn, op.Key)
	if !ok {
		return
	}
	if makeInLoop(info, fn, loop, op.Key) {
		return
	}
	if !firstresultReady(info, fn, loop, op.Key) {
		return
	}
	r, ok := straightRecvs(info, fn, loop, op.Key)
	if !ok {
		return
	}
	if n > r+kcap {
		pass.Reportf(goStmt.Pos(), msgFirstresult, n, chanName(op.Key), r, kcap)
	}
}

func makeInLoop(info *types.Info, fn ast.Node, loop ast.Stmt, k flow.Key) bool {
	call, ok := makeDef(info, fn, k)
	if !ok {
		return false
	}
	return withinNode(loop, call)
}

func firstresultReady(info *types.Info, fn ast.Node, loop ast.Stmt, k flow.Key) bool {
	if hasRange(info, fn, k) {
		return false
	}
	if handsOn(info, fn, k) || channelCopied(info, fn, k) {
		return false
	}
	if spawnDrains(info, fn, k) {
		return false
	}
	return !loopHasRecv(info, loop, k)
}

func each(pass *analysis.Pass, do func(*analysis.Pass, ast.Node, *flow.Graph)) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		fn := cur.Node()
		g := flow.NewGraph(pass, fn)
		if g == nil {
			continue
		}
		do(pass, fn, g)
	}
}

func machineReport(pass *analysis.Pass, fn ast.Node, g *flow.Graph, k flow.Key, report func(*analysis.Pass, chanOp, chanState)) {
	v := machineVisitor{pass: pass, fn: fn, info: pass.TypesInfo, key: k, report: report}
	flow.Forward(g, stUnknown,
		func(n ast.Node, s chanState) chanState { return stepNode(v.info, fn, n, k, s) },
		joinState,
		v.visitOps,
	)
}

type machineVisitor struct {
	pass   *analysis.Pass
	fn     ast.Node
	info   *types.Info
	key    flow.Key
	report func(*analysis.Pass, chanOp, chanState)
}

func (v machineVisitor) visitOps(n ast.Node, s chanState) {
	if d, ok := n.(*ast.DeferStmt); ok {
		if ck, ok := closeKey(v.info, d.Call); ok && ck == v.key {
			v.report(v.pass, chanOp{Kind: opClose, Key: ck, Pos: d.Pos(), Expr: d}, s)
			return
		}
	}
	if literalTouch(v.info, n, v.key) {
		return
	}
	for _, op := range opsOf(v.info, n, v.key) {
		if underSelect(v.fn, op.Expr) {
			op.InSelect = true
		}
		v.report(v.pass, op, s)
		s = stepOp(op, s)
	}
}
