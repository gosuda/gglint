package handoff

import "sync"

func endlessCall() { endlessCall() }

func endlessA() { endlessB() }
func endlessB() { endlessA() }

func endlessValue() int { return endlessValue() }

func endlessDefer() { defer endlessDefer() }

func releaseAfterCycle(mu *sync.Mutex) {
	endlessA()
	mu.Unlock()
}

func neverReleasesAfterCycle() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterCycle(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func neverReleasesAfterDirectRecursion() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		endlessCall()
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func releaseAfterRecursiveValue(mu *sync.Mutex) {
	println(endlessValue())
	mu.Unlock()
}

func neverReleasesAfterRecursiveValue() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterRecursiveValue(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func releaseAfterDeferredCycle(mu *sync.Mutex) {
	endlessDefer()
	mu.Unlock()
}

func neverReleasesAfterDeferredCycle() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterDeferredCycle(&mu)
	mu.Lock() // want `mu is locked again while already held`
}

func endlessLiteral() { func() { endlessA() }() }

func neverReleasesAfterLiteralCycle() {
	var mu sync.Mutex
	mu.Lock()
	go func() {
		endlessLiteral()
		mu.Unlock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func recursiveReturn(n int) {
	if n == 0 {
		return
	}
	recursiveReturn(n - 1)
}

func releaseAfterRecursiveReturn(mu *sync.Mutex) {
	recursiveReturn(2)
	mu.Unlock()
}

func releasesAfterRecursiveReturn() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterRecursiveReturn(&mu)
	mu.Lock()
	mu.Unlock()
}

func recursiveDeferredReturn(n int) {
	if n != 0 {
		defer recursiveDeferredReturn(n - 1)
	}
}

func releaseAfterRecursiveDeferredReturn(mu *sync.Mutex) {
	recursiveDeferredReturn(2)
	mu.Unlock()
}

func releasesAfterRecursiveDeferredReturn() {
	var mu sync.Mutex
	mu.Lock()
	go releaseAfterRecursiveDeferredReturn(&mu)
	mu.Lock()
	mu.Unlock()
}
