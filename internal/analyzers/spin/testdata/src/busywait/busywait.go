package busywait

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func use(any) {}

func ready() bool { return true }

type customLock struct{}

func (customLock) TryLock() bool { return true }

func busywaitReported(mu *sync.Mutex) {
	for !mu.TryLock() { // want `this loop spins until mu is free: call mu.Lock`
	}
}

func busywaitReportedGosched(mu *sync.RWMutex) {
	for !mu.TryRLock() { // want `this loop spins until mu is free: call mu.RLock`
		runtime.Gosched()
	}
}

func busywaitSilent(mu *sync.Mutex, d time.Duration) {
	for !mu.TryLock() {
		time.Sleep(d)
	}
}

func busywaitSilentPlain(n int) {
	for i := 0; i < n; i++ {
		use(i)
	}
}

func busywaitSilentNotCall() {
	done := false
	for !done {
		done = true
	}
}

func busywaitSilentPlainFunc() {
	for !ready() {
		break
	}
}

func busywaitSilentCustomLock() {
	var l customLock
	for !l.TryLock() {
	}
}

func busywaitSilentCASName(n *atomic.Int64, old int64) {
	for !n.CompareAndSwap(old, old+1) {
	}
}

func busywaitSilentTwoStmt(mu *sync.Mutex, d time.Duration) {
	n := 0
	for !mu.TryLock() {
		n++
		time.Sleep(d)
	}
	use(n)
}

func newMu() *sync.Mutex { return &sync.Mutex{} }

func busywaitSilentCallRecv() {
	for !newMu().TryLock() {
	}
}
