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
