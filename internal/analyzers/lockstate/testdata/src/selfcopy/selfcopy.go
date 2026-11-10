package selfcopy

import "sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

type PCounter struct {
	mu *sync.Mutex
}

func (c Counter) selfcopyReported() {
	c.mu.Lock() // want "locks a copy of c.mu"
	c.n++
	c.mu.Unlock()
}

func (c *Counter) selfcopySilent() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c PCounter) selfcopySilent2() {
	c.mu.Lock()
	c.mu.Unlock()
}

func (Counter) selfcopySilent3() {
}

func (c Counter) selfcopyClosureReported() {
	go func() {
		c.mu.Lock() // want "locks a copy of c.mu"
		c.n++
		c.mu.Unlock()
	}()
}
