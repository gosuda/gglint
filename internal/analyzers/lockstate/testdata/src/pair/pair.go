package pair

import "sync"

func pairReported() {
	var mu sync.RWMutex
	mu.RLock()
	mu.Unlock() // want "is not write-locked here, so this Unlock panics"
}

func pairSilent(mu *sync.Mutex) {
	mu.Unlock()
}

func pairSilent2() {
	var mu sync.Mutex
	(*sync.Mutex).Lock(&mu)
	(*sync.Mutex).Unlock(&mu)
}

func useMutex(m *sync.Mutex) {
}

func pairSilent3() {
	var mu sync.Mutex
	useMutex(&mu)
	mu.Lock()
	mu.Unlock()
}

func pairSilent4() {
	var mu sync.Mutex
	f := func() {
		mu.Lock()
		mu.Unlock()
	}
	_ = f
	mu.Lock()
	mu.Unlock()
}

func pairAliasSilent() {
	var mu sync.Mutex
	p := &mu
	p.Lock()
	mu.Unlock()
}

func pairLockerSilent() {
	var mu sync.Mutex
	var l sync.Locker = &mu
	l.Lock()
	mu.Unlock()
}

type pairS struct {
	mu sync.Mutex
}

func pairLockS(s *pairS) {
	s.mu.Lock()
}

func (s *pairS) pairLockIt() {
	s.mu.Lock()
}

func pairArgSilent() {
	var s pairS
	pairLockS(&s)
	s.mu.Unlock()
}

func pairRecvSilent() {
	var s pairS
	s.pairLockIt()
	s.mu.Unlock()
}

type literalS struct {
	mu sync.Mutex
	n  int
}

func literalOtherFieldReported() {
	var s literalS
	s.mu.Lock()
	s.mu.Unlock()
	f := func() int {
		return s.n
	}
	_ = f
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
}
