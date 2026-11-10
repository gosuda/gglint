package trylock

import "sync"

func trylockReported() {
	var mu sync.Mutex
	n := 0
	mu.TryLock() // want "the result of mu.TryLock is dropped"
	n++
	_ = n
}

func trylockReported2() {
	var mu sync.Mutex
	_ = mu.TryLock() // want "the result of mu.TryLock is dropped"
}

func trylockSilent() {
	var mu sync.Mutex
	n := 0
	if mu.TryLock() {
		n++
		mu.Unlock()
	}
	_ = n
}

func trylockAssignedSilent(mu *sync.Mutex) {
	ok := mu.TryLock()
	if ok {
		mu.Unlock()
	}
}

func trylockNestedReported(mu *sync.Mutex) {
	func() {
		mu.TryLock() // want "the result of mu.TryLock is dropped"
	}()
}
