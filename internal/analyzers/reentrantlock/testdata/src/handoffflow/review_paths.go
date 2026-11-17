package handoffflow

import "sync"

func forwardGotoRelock(retry bool) {
	var mu sync.Mutex
	mu.Lock()
	if retry {
		goto relock
	}
	return
relock:
	mu.Lock() // want `mu is locked again while already held`
}

func forwardGotoSkipsRelease() {
	var mu sync.Mutex
	mu.Lock()
	goto relock
	go mu.Unlock()
	return
relock:
	mu.Lock() // want `mu is locked again while already held`
}

func forwardGotoKeepsBranchState(retry bool) {
	var mu sync.Mutex
	mu.Lock()
	if retry {
		goto relock
	}
	go mu.Unlock()
	return
relock:
	mu.Lock() // want `mu is locked again while already held`
}

func forwardGotoAcquire(mu *sync.Mutex, retry bool) {
	if retry {
		goto acquire
	}
	return
acquire:
	mu.Lock()
}

func forwardGotoSummary(retry bool) {
	var mu sync.Mutex
	mu.Lock()
	forwardGotoAcquire(&mu, retry) // want `call to forwardGotoAcquire locks mu while it is already held`
}

func forwardGotoNested(retry, nested bool) {
	var mu sync.Mutex
	mu.Lock()
	if retry {
		if nested {
			goto relock
		}
		return
	}
	return
relock:
	lockAndValue(&mu) // want `call to lockAndValue locks mu while it is already held`
}

func forwardGotoCallback(retry bool) {
	var mu sync.Mutex
	withLock(&mu, func() {
		if retry {
			goto relock
		}
		return
	relock:
		mu.Lock() // want `mu is locked again while already held`
	})
}

func forwardGotoInvoke(mu *sync.Mutex, retry bool, callback func()) {
	mu.Lock()
	if retry {
		goto invoke
	}
	return
invoke:
	callback()
}

func forwardGotoHeldCallback(retry bool) {
	var mu sync.Mutex
	forwardGotoInvoke(&mu, retry, func() {
		mu.Lock() // want `mu is locked again while already held`
	})
}

func forwardGotoSkipsRelock() {
	var mu sync.Mutex
	mu.Lock()
	goto done
	mu.Lock()
done:
	return
}

func forwardGotoThenHandoff() {
	var mu sync.Mutex
	mu.Lock()
	goto release
	return
release:
	go mu.Unlock()
	mu.Lock()
}

func forwardGotoJoinsHeldPaths(retry bool) {
	var mu sync.Mutex
	mu.Lock()
	if retry {
		goto relock
	}
	go mu.Unlock()
	goto relock
relock:
	mu.Lock() // want `mu is locked again while already held`
}

func forwardGotoChainedLabels(retry bool) {
	var mu sync.Mutex
	mu.Lock()
	if retry {
		goto first
	}
	goto second
	return
first:
second:
	mu.Lock() // want `mu is locked again while already held`
}

func forwardGotoSurvivesDeferredLiteral(retry bool) {
	var mu sync.Mutex
	mu.Lock()
	if retry {
		goto relock
	}
	defer func() {
		goto relock
		return
	relock:
		go mu.Unlock()
	}()
	return
relock:
	mu.Lock() // want `mu is locked again while already held`
}

func switchFirstExpressionReleases() {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case func() bool { go mu.Unlock(); return true }():
		mu.Lock()
	}
}

func switchExpressionInvoke(mu *sync.Mutex, callback func()) {
	mu.Lock()
	switch {
	case func() bool { go mu.Unlock(); return true }():
		callback()
	}
}

func switchExpressionCallback() {
	var mu sync.Mutex
	switchExpressionInvoke(&mu, func() { mu.Lock() })
}

func switchFirstExpressionReleasesAtJoin(n int) {
	var mu sync.Mutex
	mu.Lock()
	switch n {
	case func() int { go mu.Unlock(); return 0 }():
		noop()
	case 1:
		noop()
	}
	mu.Lock()
}

func switchDefaultWaitsForExpressions(n int) {
	var mu sync.Mutex
	mu.Lock()
	switch n {
	default:
		mu.Lock()
	case func() int { go mu.Unlock(); return 0 }():
		mu.Lock()
	}
}

func switchLaterExpressionStaysOnUnmatchedPath(n int) {
	var mu sync.Mutex
	mu.Lock()
	switch n {
	case 0:
		mu.Lock() // want `mu is locked again while already held`
	case func() int { go mu.Unlock(); return 1 }():
		mu.Lock()
	}
}

func switchCaseListPreservesEarlierMatch(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case cond, func() bool { go mu.Unlock(); return true }():
		mu.Lock() // want `mu is locked again while already held`
	}
}

func switchCaseListSkipsFalseMatch() {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case false, func() bool { go mu.Unlock(); return true }():
		mu.Lock()
	}
}

func switchFallthroughSkipsExpression(n int) {
	var mu sync.Mutex
	mu.Lock()
	switch n {
	case 0:
		fallthrough
	case func() int { go mu.Unlock(); return 1 }():
		mu.Lock() // want `mu is locked again while already held`
	}
}

func switchFallthroughCanRelease() {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case true:
		fallthrough
	default:
		go mu.Unlock()
		mu.Lock()
	}
}

func switchMatchedCaseSkipsLaterExpressions() {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case true:
		go mu.Unlock()
		fallthrough
	case func() bool { mu.Lock(); return false }():
		mu.Lock()
	}
}

func switchConstantCaseStopsUnmatchedPath(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case cond:
		go mu.Unlock()
	case true:
		go mu.Unlock()
	default:
		mu.Lock()
	}
	mu.Lock()
}
