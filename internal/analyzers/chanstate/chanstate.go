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

func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		mk("nilop", "reports nil-channel ops in the declaring function; select cases and literals are not reported", runNilop),
		mk("doubleclose", "reports a close of an owned channel already closed on every path to it; silent for a non-local channel or one handed to other code; after a deferred or spawned close or a literal mention; after an assignment or a close on only one branch", runDoubleclose),
		mk("sendafterclose", "reports a send to an owned channel closed on every path; select sends are not reported", runSendafterclose),
		mk("selfdeadlock", "reports a send and a receive, in either order, outside select on an owned unbuffered channel no literal mentions; code after a return is checked like live code", runSelfdeadlock),
		mk("rangeunclosed", "reports a range over an owned made channel no code closes with no loop exit; silent for a field, parameter, call result, copy, or reassigned channel, for a channel handed to other code, for a spawned exit call, and for a loop body that breaks, returns, continues outward, or gotos out", runRangeunclosed),
		mk("orphansend", "reports a spawned send on an owned unbuffered channel drained only by a select that returns elsewhere; silent for a buffered, copied, handed-off, or elsewhere-drained channel, for zero or many receives or a receive outside select, for a select with no returning other case, and for sends in select or outside a literal", runOrphansend),
		mk("firstresult", "reports sends exceeding receives plus capacity; counts every receive site, so a one-branch leak is not reported. A function with a goto that jumps backward is not checked.", runFirstresult),
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
	msgFirstresult    = "this loop starts %d goroutines that send on %s, and at most %d values are received with capacity %d, so the others block forever: size the buffer to the fan-out, or receive in a loop"
)

func runNilop(pass *analysis.Pass) (any, error) {
	each(pass, func(pass *analysis.Pass, fn ast.Node, g *flow.Graph) {
		sel := selectTargets(fn)
		ctx := newFnCtx(pass.TypesInfo, fn)
		ranges := chanRanges(pass.TypesInfo, fn)
		scope := newMachineScope(pass, fn, g, reportNilop, sel)
		for _, k := range nilLocalsWith(ctx) {
			scope.reportKey(k)
			nilRangeReport(pass, ranges, k)
		}
	})
	return nil, nil
}

func nilRangeReport(pass *analysis.Pass, ranges []*ast.RangeStmt, k flow.Key) {
	info := pass.TypesInfo
	for _, r := range ranges {
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
		sel := selectTargets(fn)
		ctx := newFnCtx(pass.TypesInfo, fn)
		scope := newMachineScope(pass, fn, g, reportDoubleclose, sel)
		for _, k := range chanLocalsWith(ctx) {
			scope.reportKey(k)
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
		sel := selectTargets(fn)
		ctx := newFnCtx(pass.TypesInfo, fn)
		scope := newMachineScope(pass, fn, g, reportSendafterclose, sel)
		for _, k := range chanLocalsWith(ctx) {
			scope.reportKey(k)
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
	ctx := newFnCtx(info, fn)
	for _, k := range chanLocalsWith(ctx) {
		if !unbufferedFacts(info, ctx.facts, k) || literalMentions(info, fn, k) || ctx.facts.isCopied(k) {
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
	ctx := newFnCtx(info, fn)
	for _, r := range chanRanges(info, fn) {
		k, ok := rangeUnclosedKeyCached(ctx, r)
		if !ok {
			continue
		}
		pass.Reportf(r.X.Pos(), msgRangeunclosed, chanName(k))
	}
}

func rangeUnclosedKeyCached(ctx *fnCtx, r *ast.RangeStmt) (flow.Key, bool) {
	k, ok := rangeKeyDeclFacts(ctx, r)
	if !ok {
		return flow.Key{}, false
	}
	if rangeKeyClosedCached(ctx, r, k) {
		return flow.Key{}, false
	}
	return k, true
}

func rangeKeyDeclFacts(ctx *fnCtx, r *ast.RangeStmt) (flow.Key, bool) {
	if _, ok := ast.Unparen(r.X).(*ast.Ident); !ok {
		return flow.Key{}, false
	}
	k, ok := flow.KeyOf(ctx.info, r.X)
	if !ok || k.Path != "" || !localKey(ctx.info, ctx.fn, k) {
		return flow.Key{}, false
	}
	if _, ok := ctx.facts.makeCalls(k); !ok {
		return flow.Key{}, false
	}
	if ctx.facts.isCopied(k) || ctx.facts.isReassigned(k) {
		return flow.Key{}, false
	}
	return k, true
}

func rangeKeyClosedCached(ctx *fnCtx, r *ast.RangeStmt, k flow.Key) bool {
	if ctx.facts.isClosed(k) {
		return true
	}
	if handsOnWith(ctx.info, ctx.fn, k, ctx.spawned) {
		return true
	}
	if spawnExitsWith(ctx, k) {
		return true
	}
	return loopExits(ctx.fn, r)
}

func runOrphansend(pass *analysis.Pass) (any, error) {
	each(pass, orphansendCheck)
	return nil, nil
}

func orphansendCheck(pass *analysis.Pass, fn ast.Node, _ *flow.Graph) {
	info := pass.TypesInfo
	spawns := flow.Spawns(pass, fn)
	if len(spawns) == 0 {
		return
	}
	memo := newOrphanMemo(info, fn)
	for _, sp := range spawns {
		reportOrphanSpawns(pass, info, memo, sp)
	}
}

func reportOrphanSpawns(pass *analysis.Pass, info *types.Info, memo *orphanMemo, sp flow.Spawn) {
	for _, op := range loneSends(info, sp.Body) {
		if !memo.unbuffered(op.Key) {
			continue
		}
		if memo.facts.isCopied(op.Key) {
			continue
		}
		if memo.escaped(op.Key) || memo.drained(op.Key) {
			continue
		}
		sel, recv, ok := memo.selectRecv(op.Key)
		if !ok || !otherReturns(sel, recv.Expr) {
			continue
		}
		pass.Reportf(op.Pos, msgOrphansend, chanName(op.Key), chanName(op.Key))
	}
}

type orphanMemo struct {
	info    *types.Info
	fn      ast.Node
	facts   *bodyFacts
	spawned map[*ast.FuncLit]bool
	unb     map[flow.Key]bool
	unbDone map[flow.Key]bool
	hands   map[flow.Key]bool
	drains  map[flow.Key]bool
	done    map[flow.Key]bool
	sels    map[flow.Key]*ast.SelectStmt
	recvs   map[flow.Key]chanOp
	selOk   map[flow.Key]bool
	selDone map[flow.Key]bool
}

func newOrphanMemo(info *types.Info, fn ast.Node) *orphanMemo {
	return &orphanMemo{
		info:    info,
		fn:      fn,
		facts:   collectBodyFacts(info, fn),
		spawned: spawnLits(info, fn),
		unb:     map[flow.Key]bool{},
		unbDone: map[flow.Key]bool{},
		hands:   map[flow.Key]bool{},
		drains:  map[flow.Key]bool{},
		done:    map[flow.Key]bool{},
		sels:    map[flow.Key]*ast.SelectStmt{},
		recvs:   map[flow.Key]chanOp{},
		selOk:   map[flow.Key]bool{},
		selDone: map[flow.Key]bool{},
	}
}

func (m *orphanMemo) unbuffered(k flow.Key) bool {
	if !m.unbDone[k] {
		m.unb[k] = unbufferedFacts(m.info, m.facts, k)
		m.unbDone[k] = true
	}
	return m.unb[k]
}

func (m *orphanMemo) escaped(k flow.Key) bool {
	m.fill(k)
	return m.hands[k]
}

func (m *orphanMemo) drained(k flow.Key) bool {
	m.fill(k)
	return m.drains[k]
}

func (m *orphanMemo) fill(k flow.Key) {
	if m.done[k] {
		return
	}
	m.hands[k] = handsOnWith(m.info, m.fn, k, m.spawned)
	m.drains[k] = spawnDrainsWith(m.info, m.spawned, k)
	m.done[k] = true
}

func (m *orphanMemo) selectRecv(k flow.Key) (*ast.SelectStmt, chanOp, bool) {
	if _, ok := m.sels[k]; !ok && !m.selOk[k] {
		sel, recv, ok := loneSelectRecv(m.info, m.fn, k)
		m.sels[k] = sel
		m.recvs[k] = recv
		if ok {
			m.selOk[k] = true
		}
	}
	if !m.selOk[k] && m.sels[k] == nil {
		return nil, chanOp{}, false
	}
	return m.sels[k], m.recvs[k], m.selOk[k]
}

func runFirstresult(pass *analysis.Pass) (any, error) {
	each(pass, firstresultCheck)
	return nil, nil
}

func firstresultCheck(pass *analysis.Pass, fn ast.Node, _ *flow.Graph) {
	info := pass.TypesInfo
	ctx := newFnCtx(info, fn)
	for _, st := range countLoops(fn) {
		n, ok := loopTrip(info, st)
		if !ok || n < 2 {
			continue
		}
		for _, goStmt := range directGos(loopBody(st)) {
			firstresultGo(pass, ctx, st, goStmt, n)
		}
	}
}

type fnCtx struct {
	info     *types.Info
	fn       ast.Node
	facts    *bodyFacts
	spawned  map[*ast.FuncLit]bool
	backward map[string]bool
}

func newFnCtx(info *types.Info, fn ast.Node) *fnCtx {
	return &fnCtx{
		info:     info,
		fn:       fn,
		facts:    collectBodyFacts(info, fn),
		spawned:  spawnLits(info, fn),
		backward: backwardLabels(fn),
	}
}

func firstresultGo(pass *analysis.Pass, ctx *fnCtx, loop ast.Stmt, goStmt *ast.GoStmt, n int64) {
	lit, ok := ast.Unparen(goStmt.Call.Fun).(*ast.FuncLit)
	if !ok {
		return
	}
	op, ok := loneSend(ctx.info, lit)
	if !ok {
		return
	}
	r, kcap, ok := firstresultBound(ctx, loop, op.Key)
	if !ok {
		return
	}
	if n > r && n-r > kcap {
		pass.Reportf(goStmt.Pos(), msgFirstresult, n, chanName(op.Key), r, kcap)
	}
}

func firstresultBound(ctx *fnCtx, loop ast.Stmt, k flow.Key) (drain, kcap int64, ok bool) {
	kcap, ok = madeCapFacts(ctx.info, ctx.facts, k)
	if !ok {
		return 0, 0, false
	}
	if makeInLoopFacts(ctx.facts, loop, k) {
		return 0, 0, false
	}
	if ctx.facts.hasRanged(k) {
		return 0, 0, false
	}
	if handsOnWith(ctx.info, ctx.fn, k, ctx.spawned) || ctx.facts.isCopied(k) {
		return 0, 0, false
	}
	senders := senderLits(ctx.info, loop, k)
	drain, ok = upperBoundRecvsCached(ctx, k, senders)
	if !ok {
		return 0, 0, false
	}
	return drain, kcap, true
}

func senderLits(info *types.Info, loop ast.Stmt, k flow.Key) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	for _, g := range directGos(loopBody(loop)) {
		lit, ok := ast.Unparen(g.Call.Fun).(*ast.FuncLit)
		if !ok {
			continue
		}
		if op, ok := loneSend(info, lit); ok && op.Key == k {
			out[lit] = true
		}
	}
	return out
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

type machineMemo struct {
	targets map[ast.Node]bool
	ops     map[ast.Node][]chanOp
	touched map[ast.Node]bool
}

type machineScope struct {
	pass   *analysis.Pass
	fn     ast.Node
	graph  *flow.Graph
	report func(*analysis.Pass, chanOp, chanState)
	sel    map[ast.Node]bool
	target map[ast.Node]bool
}

func newMachineScope(pass *analysis.Pass, fn ast.Node, g *flow.Graph, report func(*analysis.Pass, chanOp, chanState), sel map[ast.Node]bool) *machineScope {
	return &machineScope{pass: pass, fn: fn, graph: g, report: report, sel: sel, target: rangeAssignTargets(fn)}
}

func (m *machineScope) reportKey(k flow.Key) {
	memo := &machineMemo{targets: m.target, ops: map[ast.Node][]chanOp{}, touched: map[ast.Node]bool{}}
	v := machineVisitor{pass: m.pass, fn: m.fn, info: m.pass.TypesInfo, key: k, report: m.report, inSelect: m.sel, memo: memo}
	flow.Forward(m.graph, stUnknown, v.step, joinState, v.visitOps)
}

type machineVisitor struct {
	pass     *analysis.Pass
	fn       ast.Node
	info     *types.Info
	key      flow.Key
	report   func(*analysis.Pass, chanOp, chanState)
	inSelect map[ast.Node]bool
	memo     *machineMemo
}

func (v machineVisitor) visitOps(n ast.Node, s chanState) {
	if v.visitSpecial(n, s) {
		return
	}
	if touchedNode(v.info, n, v.key, v.memo.touched) {
		return
	}
	v.visitEach(n, s)
}

func (v machineVisitor) visitSpecial(n ast.Node, s chanState) bool {
	if d, ok := n.(*ast.DeferStmt); ok {
		return v.visitDefer(d, s)
	}
	if g, ok := n.(*ast.GoStmt); ok {
		return v.visitGo(g, s)
	}
	return false
}

func (v machineVisitor) visitDefer(d *ast.DeferStmt, s chanState) bool {
	ck, ok := closeKey(v.info, d.Call)
	if !ok || ck != v.key {
		return false
	}
	v.report(v.pass, chanOp{Kind: opClose, Key: ck, Pos: d.Pos(), Expr: d}, s)
	return true
}

func (v machineVisitor) visitGo(g *ast.GoStmt, s chanState) bool {
	ck, ok := closeKey(v.info, g.Call)
	if !ok || ck != v.key {
		return false
	}
	v.report(v.pass, chanOp{Kind: opClose, Key: ck, Pos: g.Pos(), Expr: g}, s)
	return true
}

func (v machineVisitor) visitEach(n ast.Node, s chanState) {
	for _, op := range nodeOps(v.info, n, v.key, v.memo.ops) {
		if v.inSelect[op.Expr] {
			op.InSelect = true
		}
		v.report(v.pass, op, s)
		s = stepOp(op, s)
	}
}

func selectTargets(fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectStmt)
		if !ok {
			return true
		}
		for _, c := range sel.Body.List {
			cc, ok := c.(*ast.CommClause)
			if !ok || cc.Comm == nil {
				continue
			}
			out[cc.Comm] = true
			ast.Inspect(cc.Comm, func(m ast.Node) bool {
				if m != nil {
					out[m] = true
				}
				return true
			})
		}
		return true
	})
	return out
}
