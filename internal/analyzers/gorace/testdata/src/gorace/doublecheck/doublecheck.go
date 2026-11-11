package doublecheck

import (
	"sync"
	"sync/atomic"

	"gorace/doublecheck/dep"
)

type chkT struct{ n int }

var chkInst *chkT
var chkMu sync.Mutex

func newChk() *chkT { return &chkT{n: 1} }

func doublecheckReported() *chkT {
	if chkInst == nil { // want `chkInst is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
		chkMu.Lock()
		if chkInst == nil {
			chkInst = newChk()
		}
		chkMu.Unlock()
	}
	return chkInst
}

func doublecheckSilent() *chkT {
	chkMu.Lock()
	if chkInst == nil {
		chkInst = newChk()
	}
	chkMu.Unlock()
	return chkInst
}

type chkS struct {
	inst *chkT
	mu   sync.Mutex
}

func (s *chkS) fieldReported() *chkT {
	if s.inst == nil { // want `s.inst is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
		s.mu.Lock()
		if s.inst == nil {
			s.inst = newChk()
		}
		s.mu.Unlock()
	}
	return s.inst
}

func doublecheckHeldSilent() *chkT {
	chkMu.Lock()
	defer chkMu.Unlock()
	if chkInst == nil {
		chkInst = newChk()
	}
	return chkInst
}

func doublecheckReversedReported() *chkT {
	if nil == chkInst { // want `chkInst is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
		chkMu.Lock()
		if chkInst == nil {
			chkInst = newChk()
		}
		chkMu.Unlock()
	}
	return chkInst
}

func doublecheckImportedReported() {
	if dep.Inst == nil { // want `dep.Inst is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
		chkMu.Lock()
		if dep.Inst == nil {
			dep.Inst = &dep.T{N: 1}
		}
		chkMu.Unlock()
	}
}

type atomicHolder struct {
	mu sync.Mutex
	n  *atomic.Int64
}

func (s *atomicHolder) doublecheckAtomicReported() *atomic.Int64 {
	if s.n == nil { // want `s.n is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
		s.mu.Lock()
		if s.n == nil {
			s.n = new(atomic.Int64)
		}
		s.mu.Unlock()
	}
	return s.n
}

func doublecheckNestedReported() *chkT {
	f := func() *chkT {
		if chkInst == nil { // want `chkInst is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
			chkMu.Lock()
			if chkInst == nil {
				chkInst = newChk()
			}
			chkMu.Unlock()
		}
		return chkInst
	}
	return f()
}

func doublecheckLocalSilent() *chkT {
	var s chkS
	if s.inst == nil {
		s.mu.Lock()
		if s.inst == nil {
			s.inst = newChk()
		}
		s.mu.Unlock()
	}
	return s.inst
}

var chkRW sync.RWMutex

func doublecheckRelockSilent() *chkT {
	chkRW.RLock()
	if chkInst == nil {
		chkRW.RUnlock()
		chkRW.Lock()
		if chkInst == nil {
			chkInst = newChk()
		}
		chkRW.Unlock()
		return chkInst
	}
	chkRW.RUnlock()
	return chkInst
}
