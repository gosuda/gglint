package upgrade

import "sync"

func upgradeReported() {
	var mu sync.RWMutex
	mu.RLock()
	mu.Lock() // want "is read-locked by this goroutine, so this Lock waits for itself"
}

func upgradeSilent() {
	var mu sync.RWMutex
	mu.RLock()
	mu.RUnlock()
	mu.Lock()
}

func upgradeAliasSilent() {
	var mu sync.RWMutex
	mu.RLock()
	p := &mu
	p.RUnlock()
	mu.Lock()
}

type upgradeR struct {
	mu sync.RWMutex
}

func (r *upgradeR) upgradeUnlockR() {
	r.mu.RUnlock()
}

func upgradeEnclosingSilent() {
	var r upgradeR
	r.mu.RLock()
	r.upgradeUnlockR()
	r.mu.Lock()
}

func upgradeMethodValueSilent() {
	var mu sync.RWMutex
	mu.RLock()
	runlock := mu.RUnlock
	runlock()
	mu.Lock()
}

func upgradeGoCallSilent() {
	var mu sync.RWMutex
	mu.RLock()
	go mu.Lock()
	mu.RUnlock()
}

func upgradeDeepReported() {
	var mu sync.RWMutex
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.Lock() // want "is read-locked by this goroutine, so this Lock waits for itself"
}

func upgradeDeepRUnlockReported() {
	var mu sync.RWMutex
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RLock()
	mu.RUnlock()
	mu.Lock() // want "is read-locked by this goroutine, so this Lock waits for itself"
}
