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
	var mu sync.Mutex
	for _, x := range xs {
		mu.Lock()
		_ = x
		mu.Unlock()
	}
}

func deferloopSilent3(xs []int) {
	for _, x := range xs {
		defer println(x)
	}
}

func deferloopFreshSilent(n int) {
	for range n {
		var mu sync.Mutex
		mu.Lock()
		defer mu.Unlock()
	}
}

func deferloopUnlockedSilent(xs []int) {
	var mu sync.RWMutex
	for i := 0; i < len(xs); i++ {
		mu.RLock()
		defer mu.RUnlock()
		mu.RLock()
		mu.RUnlock()
	}
}

func deferloopNoLockSilent(mu *sync.Mutex) {
	mu.Lock()
	for range 1 {
		defer mu.Unlock()
	}
}

func nextItem() (int, bool) {
	return 0, false
}

func deferloopLeavesSilent() {
	var mu sync.Mutex
	for {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := nextItem(); !ok {
			break
		}
		panic("unexpected item")
	}
}
