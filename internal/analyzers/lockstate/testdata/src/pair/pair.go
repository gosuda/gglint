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

func pairMethodValueSilent() {
	var mu sync.Mutex
	lock := mu.Lock
	lock()
	mu.Unlock()
}

func pairAliasedPointerSilent() {
	p := &pairS{}
	p.mu.Lock()
	q := p
	q.mu.Unlock()
}

func pairCopiedLockSilent() {
	var mu sync.Mutex
	mu.Lock()
	cp := mu
	cp.Unlock()
	mu.Unlock()
}

func pairUnlockNeverLockedReported() int {
	var s literalS
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	return s.n
}

func pairDeferTrySilent() {
	var mu sync.Mutex
	defer reportDeferTry(mu.TryLock())
	mu.Unlock()
}

func reportDeferTry(bool) {}

func pairRecursiveReadSilent() {
	var mu sync.RWMutex
	mu.RLock()
	mu.RLock()
	mu.RUnlock()
	mu.RUnlock()
}

func pairDoubleRUnlockReported() {
	var mu sync.RWMutex
	mu.RLock()
	mu.RUnlock()
	mu.RUnlock() // want "is not read-locked here, so this RUnlock panics"
}

func pairInexactTakeTwoReads(mu *sync.RWMutex) {
	mu.RLock()
	mu.RLock()
}

func pairInexactRUnlockSilent(mu *sync.RWMutex) {
	pairInexactTakeTwoReads(mu)
	mu.RLock()
	mu.RUnlock()
	mu.RUnlock()
	mu.RUnlock()
}
func pairNestedReported(mu *sync.RWMutex) {
	mu.RLock()
	mu.RLock()
	mu.RUnlock()
	mu.Unlock() // want "is not write-locked here, so this Unlock panics"
}

func newLockedPairS() pairS {
	var s pairS
	s.mu.Lock()
	return s
}

func pairReassignedSilent() {
	var s pairS
	s = newLockedPairS()
	s.mu.Unlock()
}

func pairReassignBeforeReported(p *pairS) int {
	var s pairS
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	s = *p
	return 0
}

func pairVarEmptyLiteralReported() int {
	var s literalS = literalS{}
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	return s.n
}

func pairVarEmptyMutexReported() {
	var mu sync.RWMutex = sync.RWMutex{}
	mu.RUnlock() // want "is not read-locked here, so this RUnlock panics"
}

func pairVarLockedValueSilent() {
	var s pairS = newLockedPairS()
	s.mu.Unlock()
}

func pairVarEmptyThenReassignedReported() {
	var s pairS = pairS{}
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	s = newLockedPairS()
}

func pairVarLockedThenReassignedSilent() {
	var s pairS = newLockedPairS()
	s.mu.Unlock()
	s = pairS{}
}

func pairShortLockedThenReassignedSilent() {
	s := newLockedPairS()
	s.mu.Unlock()
	s = pairS{}
}

func pairVarEmptyThenEmptyReported() {
	var s pairS = pairS{}
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	s = pairS{}
}

func pairShortEmptyThenEmptyReported() {
	s := pairS{}
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	s = pairS{}
}

func newLockedPairS2() (pairS, error) {
	var s pairS
	s.mu.Lock()
	return s, nil
}

func pairShortRedeclSilent() {
	s := pairS{}
	s, err := newLockedPairS2()
	if err != nil {
		return
	}
	s.mu.Unlock()
}

func pairShortRedeclAfterReported() {
	s := pairS{}
	s.mu.Unlock() // want "is not write-locked here, so this Unlock panics"
	s, err := newLockedPairS2()
	if err != nil {
		return
	}
	_ = s
}
