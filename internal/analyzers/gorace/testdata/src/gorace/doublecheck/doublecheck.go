package doublecheck

import "sync"

type chkT struct{}

var chkInst *chkT
var chkMu sync.Mutex

func newChk() *chkT { return &chkT{} }

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
		chkMu.Lock()
		if chkInst == nil {
			chkInst = newChk()
		}
		chkMu.Unlock()
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

func doublecheckCallReported() *chkT {
	fn := func() {}
	if chkInst == nil { // want `chkInst is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once`
		fn()
		chkMu.Lock()
		if chkInst == nil {
			chkInst = newChk()
		}
		chkMu.Unlock()
	}
	return chkInst
}
