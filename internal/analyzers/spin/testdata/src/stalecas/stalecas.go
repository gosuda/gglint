package stalecas

import "sync/atomic"

func use(any) {}

func myCAS(x *int32, o, n int32) bool { return o == n }

func oneArg(x int64) bool { return x > 0 }

type casBox struct{ v int64 }

func (c *casBox) CompareAndSwap(o, n int64) bool { return o == n }

func (c *casBox) Check(o int64) bool { return o == c.v }

const stalecasOld = int64(1)

func stalecasReported(n *atomic.Int64) {
	old := n.Load()
	for !n.CompareAndSwap(old, old+1) { // want `after one failed swap old is stale, so every retry fails: load it again inside the loop`
	}
}

func stalecasReportedIfBreak(n *atomic.Int64) {
	old := n.Load()
	for { // want `after one failed swap old is stale, so every retry fails: load it again inside the loop`
		if n.CompareAndSwap(old, old+1) {
			break
		}
	}
}

func stalecasReportedReturn(n *atomic.Int64) int {
	old := n.Load()
	for { // want `after one failed swap old is stale, so every retry fails: load it again inside the loop`
		if n.CompareAndSwap(old, old+1) {
			return 1
		}
	}
}

func stalecasReportedFunc3(x *int32) {
	old := atomic.LoadInt32(x)
	for !atomic.CompareAndSwapInt32(x, old, old+1) { // want `after one failed swap old is stale, so every retry fails: load it again inside the loop`
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
		}
	}
	use(old)
}

func stalecasSilentUseIf(n *atomic.Int64) {
	old := n.Load()
	for {
		if n.CompareAndSwap(old, old+1) {
			use(old)
		}
	}
}

func stalecasSilentWrongPkg(x *int32) {
	old := atomic.LoadInt32(x)
	for !myCAS(x, old, old+1) {
	}
}

func stalecasSilentNotCall(n *atomic.Int64) {
	old := n.Load()
	for {
		if old > 0 {
			break
		}
	}
	use(old)
}

func stalecasSilentMethodWrongRecv() {
	c := &casBox{}
	old := int64(0)
	for !c.CompareAndSwap(old, old+1) {
	}
}

func stalecasSilentOneArg(n *atomic.Int64) {
	old := n.Load()
	for {
		if oneArg(old) {
			break
		}
	}
	use(old)
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

func stalecasSilentOldParam(n *atomic.Int64, old int64) {
	for !n.CompareAndSwap(old, old+1) {
	}
}

func stalecasSilentOldConst(n *atomic.Int64) {
	for !n.CompareAndSwap(stalecasOld, stalecasOld+1) {
	}
}

func stalecasSilentEmpty() {
	for {
	}
}

func stalecasSilentNonIf(n *atomic.Int64) {
	old := n.Load()
	for {
		use(old)
	}
	use(old)
}

func stalecasSilentElse(n *atomic.Int64) {
	old := n.Load()
	for {
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
	olds := []int64{old}
	for !n.CompareAndSwap(old, old+1) {
		for _, old = range olds {
		}
	}
}
