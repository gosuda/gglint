package handoff

import (
	"sync"
	"sync/atomic"
)

func release(mu *sync.Mutex) { mu.Unlock() }

func lockAndRelease(mu *sync.Mutex) {
	mu.Lock()
	mu.Unlock()
}

func direct() {
	var mu sync.Mutex
	mu.Lock()
	go mu.Unlock()
	mu.Lock()
	mu.Unlock()
}

func named() {
	var mu sync.Mutex
	mu.Lock()
	go release(&mu)
	mu.Lock()
	mu.Unlock()
}

func literal() {
	var mu sync.Mutex
	mu.Lock()
	go func() { release(&mu) }()
	mu.Lock()
	mu.Unlock()
}

type wakeMutex struct {
	sync.Mutex
	woken atomic.Bool
}

func (m *wakeMutex) wake() bool {
	if m.woken.CompareAndSwap(false, true) {
		m.Unlock()
		return true
	}
	return false
}

// Reduced from modernc.org/sqlite's TestMutexWakeOnce: only one notifier
// releases the acquired mutex, and the second Lock waits for that notifier.
func wakeOnce() {
	var m wakeMutex
	m.Lock()
	woke := make(chan bool, 2)
	for j := 0; j < 2; j++ {
		go func() { woke <- m.wake() }()
	}
	m.Lock()
	<-woke
	<-woke
	m.Unlock()
}

func methodExpression() {
	var m wakeMutex
	m.Lock()
	go (*wakeMutex).wake(&m)
	m.Lock()
	m.Unlock()
}

func releaseEventually(mu *sync.Mutex, n int) {
	if n == 0 {
		mu.Unlock()
		return
	}
	releaseNext(mu, n-1)
}

func releaseNext(mu *sync.Mutex, n int) { releaseEventually(mu, n) }

func transitive() {
	var mu sync.Mutex
	mu.Lock()
	go releaseNext(&mu, 2)
	mu.Lock()
	mu.Unlock()
}

func delayedRelease(mu *sync.Mutex) { defer mu.Unlock() }

func deferredInGoroutine() {
	var mu sync.Mutex
	mu.Lock()
	go delayedRelease(&mu)
	mu.Lock()
	mu.Unlock()
}

func reacquired() {
	var mu sync.Mutex
	mu.Lock()
	go release(&mu)
	mu.Lock()
	mu.Lock() // want `mu is locked again while already held`
}

func otherMutex() {
	var mu, other sync.Mutex
	mu.Lock()
	other.Lock()
	go release(&other)
	mu.Lock() // want `mu is locked again while already held`
}

func acquiredBeforeRelease() {
	var mu sync.Mutex
	mu.Lock()
	go lockAndRelease(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func acquiredInLiteral() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		mu.Lock()
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func releaseAfterAliasLock(mu *sync.Mutex) {
	alias := mu
	alias.Lock()
	mu.Unlock()
}

func aliasedAcquire() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterAliasLock(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func pointer(mu *sync.Mutex) *sync.Mutex { return mu }

func releaseAfterIndirectLock(mu *sync.Mutex) {
	pointer(mu).Lock()
	mu.Unlock()
}

func indirectAcquire() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterIndirectLock(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func storedRelease() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		releaseLater := func() { mu.Unlock() }
		_ = releaseLater
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func deferredInParent() {
	var mu sync.Mutex
	mu.Lock()
	defer release(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func lockAndValue(mu *sync.Mutex) int {
	mu.Lock()
	return 1
}

func releaseWithValue(mu *sync.Mutex, _ int) { mu.Unlock() }

func argumentBeforeSpawn() {
	var mu sync.Mutex
	mu.Lock()
	go releaseWithValue(&mu, lockAndValue(&mu)) // want `call to lockAndValue locks mu while it is already held`
}

func withLock(mu *sync.Mutex, callback func()) {
	mu.Lock()
	defer mu.Unlock()
	callback()
}

func callbackHandoff() {
	var mu sync.Mutex
	withLock(&mu, func() {
		go release(&mu)
		mu.Lock()
	})
}

func callbackAfterHandoff(mu *sync.Mutex, callback func()) {
	mu.Lock()
	go release(mu)
	callback()
}

func callbackNoLongerHeld() {
	var mu sync.Mutex
	callbackAfterHandoff(&mu, func() {
		mu.Lock()
		mu.Unlock()
	})
}
