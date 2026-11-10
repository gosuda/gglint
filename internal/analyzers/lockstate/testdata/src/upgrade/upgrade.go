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
