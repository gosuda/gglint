package deferloop

import "sync"

func deferloopReported(xs []int) {
	var mu sync.Mutex
	for _, x := range xs {
		mu.Lock()
		defer mu.Unlock() // want "the deferred unlock runs at function exit"
		_ = x
	}
}

func deferloopSilent(xs []int) int {
	var mu sync.Mutex
	for _, x := range xs {
		mu.Lock()
		defer mu.Unlock()
		_ = x
		return 1
	}
	return 0
}

func deferloopSilent2(xs []int) {
	var mu sync.RWMutex
	for _, x := range xs {
		mu.Lock()
		defer mu.RLock()
		_ = x
	}
}

func deferloopSilent3(xs []int) {
	var mu sync.Mutex
	for _, x := range xs {
		_ = x
		defer mu.Unlock()
	}
}
