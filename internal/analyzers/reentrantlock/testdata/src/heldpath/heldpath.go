package heldpath

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

type box struct {
	inner counter
}

func withLock(l *sync.Mutex, fn func()) {
	l.Lock()
	defer l.Unlock()
	fn()
}

func incInner(b *box) {
	b.inner.inc()
}

func heldThroughHelper() {
	var b box
	withLock(&b.inner.mu, func() {
		incInner(&b) // want `^call to incInner locks b\.inner\.mu while it is already held`
	})
}
