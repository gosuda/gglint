package reentrantlock

import "sync"

type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *counter) relock() {
	c.mu.Lock() // want `c\.mu is locked again while already held`
	c.mu.Unlock()
}

type wrapped struct {
	counter
	mu sync.Mutex
}

type guarded struct {
	sync.Mutex
	n int
}

func withLock(mu *sync.Mutex, fn func()) {
	mu.Lock()
	defer mu.Unlock()
	fn()
}

func lockPtr(mu *sync.Mutex) {
	mu.Lock()
	mu.Unlock()
}

func lockCopy(mu sync.Mutex) {
	mu.Lock()
	mu.Unlock()
}

func lockAndReturn(mu *sync.Mutex) int {
	lockPtr(mu)
	return 0
}

func sink(int) {}

func ping(mu *sync.Mutex, n int) {
	if n > 0 {
		pong(mu, n-1)
	}
}

func pong(mu *sync.Mutex, n int) {
	lockPtr(mu)
	ping(mu, n)
}

func directRelock() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	mu.Lock() // want `mu is locked again while already held: sync mutexes are not reentrant, so this deadlocks`
}

func rwRelock() {
	var rw sync.RWMutex
	rw.Lock()
	defer rw.Unlock()
	rw.Lock() // want `rw is locked again while already held`
}

func embeddedRelock() {
	var g guarded
	g.Lock()
	defer g.Unlock()
	g.Mutex.Lock() // want `g\.Mutex is locked again while already held`
}

func transitiveParameter() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	lockAndReturn(&mu) // want `call to lockAndReturn locks mu while it is already held: sync mutexes are not reentrant, so this deadlocks`
}

func mutualRecursion() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	ping(&mu, 2) // want `call to ping locks mu while it is already held`
}

func literalCallback() {
	var mu sync.Mutex
	withLock(&mu, func() {
		mu.Lock() // want `mu is locked again while already held`
		mu.Unlock()
	})
}

func methodValueCallback() {
	var c counter
	withLock(&c.mu, c.relock)
}

func goArgument() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	go sink(lockAndReturn(&mu)) // want `call to lockAndReturn locks mu while it is already held`
}

func promotedMethodSameMutex() {
	var w wrapped
	w.counter.mu.Lock()
	defer w.counter.mu.Unlock()
	w.inc() // want `call to inc locks w\.counter\.mu while it is already held`
}

func promotedMethodOwnMutex() {
	var w wrapped
	w.mu.Lock()
	defer w.mu.Unlock()
	w.inc()
}

func unlockedBetween() {
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
	mu.Lock()
	mu.Unlock()
	lockPtr(&mu)
}

func otherMutex() {
	var a, b sync.Mutex
	a.Lock()
	defer a.Unlock()
	lockPtr(&b)
}

func valueParameter() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	lockCopy(mu)
}

func spawnedAndStored() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	go lockPtr(&mu)
	go func() {
		lockPtr(&mu)
	}()
	stored := func() {
		lockPtr(&mu)
	}
	_ = stored
}
