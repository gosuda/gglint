package handoff

import (
	atomic "handoffatomic"
	"handoffext"
	"sort"
	"sync"
	stdatomic "sync/atomic"
	"unsafe"
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
	woken stdatomic.Bool
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

func readAcquireBeforeRelease() {
	var mu sync.RWMutex
	mu.Lock()
	go func() {
		mu.RLock()
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func callbackThenRelease(mu *sync.Mutex, before func()) {
	before()
	mu.Unlock()
}

func unknownCallback() {
	var mu sync.Mutex
	mu.Lock()
	before := func() { mu.Lock() }
	go callbackThenRelease(&mu, before)
	mu.Lock() // want `mu is locked again while already held`
}

func indirectCallback() {
	var mu sync.Mutex
	mu.Lock()
	before := func() { mu.Lock() }
	go func() {
		before()
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func onceCallback() {
	var mu sync.Mutex
	var once sync.Once
	mu.Lock()
	go func() {
		once.Do(mu.Lock)
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func sortCallback() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		sort.Slice([]int{2, 1}, func(i, j int) bool {
			mu.Lock()
			return i < j
		})
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func importedAcquire() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		handoffext.Lock(&mu)
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func atomicNameSpoof() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		atomic.Store(&mu)
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

type valueReceiver struct{ mu sync.Mutex }

func (v valueReceiver) release() { v.mu.Unlock() }

func copiedReceiver() {
	var v valueReceiver
	v.mu.Lock()
	go v.release()
	v.mu.Lock() // want `v\.mu is locked again while already held`
}

func copiedMethodExpression() {
	var v valueReceiver
	v.mu.Lock()
	go valueReceiver.release(v)
	v.mu.Lock() // want `v\.mu is locked again while already held`
}

func releaseRebound(mu, other *sync.Mutex) {
	mu = other
	mu.Unlock()
}

func reboundParameter() {
	var first, second sync.Mutex
	first.Lock()
	second.Lock()
	go releaseRebound(&first, &second)
	first.Lock() // want `first is locked again while already held`
}

type pointerReceiver struct{ sync.Mutex }

func (p *pointerReceiver) release(other *pointerReceiver) {
	p = other
	p.Unlock()
}

func reboundReceiver() {
	var first, second pointerReceiver
	first.Lock()
	second.Lock()
	go first.release(&second)
	first.Lock() // want `first\.Mutex is locked again while already held`
}

func releaseThroughAlias(mu, other *sync.Mutex) {
	slot := &mu
	*slot = other
	mu.Unlock()
}

func aliasParameter() {
	var first, second sync.Mutex
	first.Lock()
	second.Lock()
	go releaseThroughAlias(&first, &second)
	first.Lock() // want `first is locked again while already held`
}

func replacePointer(slot **sync.Mutex, other *sync.Mutex) { *slot = other }

func releaseReplaced(mu, other *sync.Mutex) {
	replacePointer(&mu, other)
	mu.Unlock()
}

func replacedParameter() {
	var first, second sync.Mutex
	first.Lock()
	second.Lock()
	go releaseReplaced(&first, &second)
	first.Lock() // want `first is locked again while already held`
}

func releaseThroughAtomic(mu, other *sync.Mutex) {
	stdatomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&mu)), unsafe.Pointer(other))
	mu.Unlock()
}

func atomicParameter() {
	var first, second sync.Mutex
	first.Lock()
	second.Lock()
	go releaseThroughAtomic(&first, &second)
	first.Lock() // want `first is locked again while already held`
}

func releaseThroughSpawn(mu, other *sync.Mutex, done chan struct{}) {
	go func() {
		mu = other
		close(done)
	}()
	<-done
	mu.Unlock()
}

func spawnedReplacement() {
	var first, second sync.Mutex
	first.Lock()
	second.Lock()
	go releaseThroughSpawn(&first, &second, make(chan struct{}))
	first.Lock() // want `first is locked again while already held`
}

// The raw storage's identity is unknown. Keep the existing finding instead of
// treating this unsafe operation as a proved release-only handoff.
func opaqueUnsafeSource(raw unsafe.Pointer) {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		stdatomic.StoreUintptr((*uintptr)(raw), 0)
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func releaseAfterIteration(mu *sync.Mutex, iterator func(func() bool)) {
	for range iterator {
	}
	mu.Unlock()
}

func rangeCallback() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterIteration(&mu, func(func() bool) { mu.Lock() })
	mu.Lock() // want `mu is locked again while already held`
}

func releaseAfterLoop(mu *sync.Mutex) {
	for {
	}
	mu.Unlock()
}

func neverReleasesAfterLoop() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterLoop(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func releaseAfterSelect(mu *sync.Mutex) {
	select {}
	mu.Unlock()
}

func neverReleasesAfterSelect() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterSelect(&mu)
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
