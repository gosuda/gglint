package stalecas

import "sync/atomic"

func use(any) {}

func myCAS(x *int32, o, n int32) bool { if *x != o { return false }; *x = n; return true }

func oneArg(x int64) bool { return x > 0 }

type casBox struct{ v int64 }

func (c *casBox) CompareAndSwap(o, n int64) bool { if c.v != o { return false }; c.v = n; return true }

func (c *casBox) Check(o int64) bool { return o == c.v }

const stalecasOld = int64(1)

func stalecasReported(n *atomic.Int64) {
	old := n.Load()
for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
	}
}

func stalecasReportedIfBreak(n *atomic.Int64) {
	old := n.Load()
	for { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if n.CompareAndSwap(old, old+1) {
			break
		}
	}
}

func stalecasReportedReturn(n *atomic.Int64) int {
	old := n.Load()
	for { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if n.CompareAndSwap(old, old+1) {
			return 1
		}
	}
}

func stalecasReportedFunc3(x *int32) {
	old := atomic.LoadInt32(x)
	for !atomic.CompareAndSwapInt32(x, old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
	}
}

func stalecasSilent(n *atomic.Int64) {
	for {
		old := n.Load()
		if n.CompareAndSwap(old, old+1) {
			break
		}
	}
}

func stalecasSilentBodyAssign(n *atomic.Int64) {
	old := n.Load()
	x := 0
	for !n.CompareAndSwap(old, old+1) {
		old = n.Load()
		x++
	}
	use(x)
}

func stalecasSilentPostAssign(n *atomic.Int64) {
	old := n.Load()
	for ; !n.CompareAndSwap(old, old+1); old = n.Load() {
	}
}

func stalecasSilentTwoStmtIf(n *atomic.Int64) {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			use(1)
			use(2)
			break
		}
	}
	use(old)
}

func stalecasSilentUseIf(n *atomic.Int64) {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			use(old)
			break
		}
	}
}

func stalecasSilentWrongPkg(x *int32) {
	old := atomic.LoadInt32(x)
	for !myCAS(x, old, old+1) {
	}
}


func stalecasSilentMethodWrongRecv() {
	c := &casBox{}
	old := int64(0)
	for !c.CompareAndSwap(old, old+1) {
	}
}


func stalecasSilentNonIdentOld(n *atomic.Int64) {
	for !n.CompareAndSwap(n.Load(), 2) {
	}
}

func stalecasSilentMethodOneArg() {
	c := &casBox{}
	old := int64(0)
	for {
		if c.Check(old) {
			break
		}
	}
	use(old)
}


func stalecasSilentNonIf(n *atomic.Int64) {
	old := n.Load()
	for {
		use(old)
		break
	}
	use(old)
}

func stalecasReportedElse(n *atomic.Int64) {
	old := n.Load()
	for { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if n.CompareAndSwap(old, old+1) {
			break
		} else {
			use(old)
		}
	}
}

func stalecasSilentInit(n *atomic.Int64) {
	for {
		if x := n.Load(); n.CompareAndSwap(x, x+1) {
			break
		}
	}
}

func stalecasSilentRange(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) {
		olds := []int64{n.Load()}
		for _, old = range olds {
		}
	}
}
func stalecasReportedCondFallback(n *atomic.Int64, ready bool) {
	old := n.Load()
	for !ready { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if n.CompareAndSwap(old, old+1) {
			break
		}
	}
}

func stalecasReportedLoopInit(n *atomic.Int64) {
	for old := n.Load(); !n.CompareAndSwap(old, old+1); { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
	}
}

func stalecasReportedIfInit(n *atomic.Int64) {
	old := n.Load()
	i := 0
	for { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if i = 1; n.CompareAndSwap(old, old+1) {
			break
		}
	}
	use(i)
}

func stalecasSilentElseReturn(n *atomic.Int64) int {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			break
		} else {
			return 0
		}
	}
	return 1
}

func stalecasSilentClosure(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) {
		reload := func() { old = n.Load() }
		reload()
	}
	use(old)
}

func loadOld(p *int64, n *atomic.Int64) { *p = n.Load() }

func stalecasSilentAddr(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) {
		loadOld(&old, n)
	}
	use(old)
}

func stalecasReportedReadCapture(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		show := func() { use(old) }
		show()
	}
	use(old)
}

func stalecasSilentElseIf(n *atomic.Int64, c bool) int {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			break
		} else if c {
			return 0
		} else {
			return 1
		}
	}
	return 2
}

func stalecasSilentElseBreak(n *atomic.Int64) {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			break
		} else {
			break
		}
	}
	use(old)
}

func stalecasSilentElsePanic(n *atomic.Int64) {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			break
		} else {
			panic("stale")
		}
	}
}

func stalecasSilentElseBlock(n *atomic.Int64) int {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			break
		} else {
			{
				return 0
			}
		}
	}
	return 1
}

func stalecasSilentClosureBefore(n *atomic.Int64) {
	old := n.Load()
	bump := func() { old = n.Load() }
	for !n.CompareAndSwap(old, old+1) {
		bump()
	}
	use(old)
}

func stalecasSilentAddrBefore(n *atomic.Int64) {
	old := n.Load()
	p := &old
	for !n.CompareAndSwap(old, old+1) {
		loadOld(p, n)
	}
	use(old)
}
func stalecasSilentClosureAfter(n *atomic.Int64) {
	old := n.Load()
	reset := func() { old = n.Load() }
	for !n.CompareAndSwap(old, old+1) {
		reset()
	}
	reset()
}

func stalecasSilentAddrAfter(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) {
		loadOld(&old, n)
	}
	use(&old)
}
func stalecasSilentRangeOuter(n *atomic.Int64) {
	old := n.Load()
	for range 2 {
		reload := func() { old = n.Load() }
		for !n.CompareAndSwap(old, old+1) {
			reload()
		}
	}
	use(old)
}
func stalecasReportedAfterLabel(n *atomic.Int64, xs []int) {
	old := n.Load()
scan:
	for _, x := range xs {
		if x < 0 {
			break scan
		}
	}
	for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
	}
}

func stalecasSilentInRange(n *atomic.Int64, xs []int) {
	old := n.Load()
	sink := func(p *int64) { use(*p) }
	for _, x := range xs {
		for !n.CompareAndSwap(old, old+int64(x)) {
			loadOld(&old, n)
		}
		sink(&old)
	}
}

type staleTask struct{ fn func() }

func (t staleTask) run() { t.fn() }

func stalecasSilentMethodReload(n *atomic.Int64) {
	old := n.Load()
	bump := func() { old = n.Load() }
	t := staleTask{fn: bump}
	for !n.CompareAndSwap(old, old+1) {
		t.run()
	}
	use(old)
}

type staleReloader struct {
	dst *int64
	src *atomic.Int64
}

func (r staleReloader) reload() { *r.dst = r.src.Load() }

func stalecasSilentCompositeReload(n *atomic.Int64) {
	old := n.Load()
	r := staleReloader{dst: &old, src: n}
	for !n.CompareAndSwap(old, old+1) {
		r.reload()
	}
	use(old)
}

func stalecasSilentGotoReload(n *atomic.Int64) {
	var old int64
again:
	old = n.Load()
	for !n.CompareAndSwap(old, old+1) {
		goto again
	}
	use(old)
}
func stalecasReportedClosureCall(n *atomic.Int64) {
	func() {
		old := n.Load()
		for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
			use(1)
		}
		use(old)
	}()
}

func stalecasReportedSelfLabel(n *atomic.Int64) {
	old := n.Load()
outer:
	for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if oneArg(1) {
			continue outer
		}
	}
	use(old)
}

func stalecasReportedInnerGoto(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		if oneArg(1) {
			goto skip
		}
		use(1)
	skip:
		use(2)
	}
	use(old)
}
func stalecasSilentSelfGotoInit(n *atomic.Int64) {
retry:
	for old := n.Load(); !n.CompareAndSwap(old, old+1); {
		if oneArg(1) {
			goto retry
		}
	}
}

type staleNode struct{ count int }

func stalecasReportedAddrField(h *atomic.Pointer[staleNode], n *staleNode) *int {
	old := h.Load()
	for !h.CompareAndSwap(old, n) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
	}
	return &old.count
}
func stalecasReportedFieldLitAssign(h *atomic.Pointer[staleNode], n *staleNode) {
	old := h.Load()
	bump := func() { old.count = 0 }
	_ = bump
	for !h.CompareAndSwap(old, n) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
	}
}
type staleVal struct{ n int }

func stalecasSilentValueFieldLitAssign(v *atomic.Value) {
	v.Store(staleVal{n: 1})
	old := staleVal{}
	bump := func() { old.n++ }
	for !v.CompareAndSwap(old, staleVal{n: 2}) {
		bump()
	}
	use(old)
}

func staleValRefresh(p *int, v *atomic.Value) { *p = v.Load().(staleVal).n }

func stalecasSilentValueAddrField(v *atomic.Value) {
	v.Store(staleVal{n: 1})
	old := staleVal{}
	for !v.CompareAndSwap(old, staleVal{n: 2}) {
		staleValRefresh(&old.n, v)
	}
	use(old)
}

type staleInner struct{ f int }

type staleOuter struct{ *staleInner }

func stalecasReportedEmbeddedPointer(v *atomic.Value) {
	v.Store(staleOuter{staleInner: &staleInner{f: 1}})
	old := staleOuter{staleInner: &staleInner{}}
	bump := func() { old.f++ }
	_ = bump
	for !v.CompareAndSwap(old, staleOuter{staleInner: &staleInner{}}) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		bump()
	}
	use(old)
}

type staleCell struct{ n int }

func (s *staleCell) reload(v *atomic.Value) { *s = v.Load().(staleCell) }

func stalecasSilentPointerMethod(v *atomic.Value) {
	v.Store(staleCell{n: 1})
	old := staleCell{}
	for !v.CompareAndSwap(old, staleCell{n: 2}) {
		old.reload(v)
	}
	use(old)
}

func (i *staleInner) reset() { i.f = 0 }

func stalecasReportedEmbeddedPointerMethod(v *atomic.Value) {
	v.Store(staleOuter{staleInner: &staleInner{f: 1}})
	old := staleOuter{staleInner: &staleInner{}}
	for !v.CompareAndSwap(old, staleOuter{staleInner: &staleInner{}}) { // want `after one failed swap old is stale, so retries fail until another goroutine stores old back: load it again inside the loop`
		old.reset()
	}
	use(old)
}

type staleVBox struct{ n int }

func (s *staleVBox) reload(v *atomic.Value) { *s = v.Load().(staleVOuter).staleVBox }

type staleVOuter struct{ staleVBox }

func stalecasSilentEmbeddedValueMethod(v *atomic.Value) {
	v.Store(staleVOuter{staleVBox{n: 1}})
	old := staleVOuter{}
	for !v.CompareAndSwap(old, staleVOuter{staleVBox{n: 2}}) {
		old.reload(v)
	}
	use(old)
}
