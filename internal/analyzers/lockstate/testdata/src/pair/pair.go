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
