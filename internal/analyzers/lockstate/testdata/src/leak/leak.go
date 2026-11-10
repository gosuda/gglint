package leak

import (
	"errors"
	"sync"
)

type S struct {
	mu sync.Mutex
}

var errBad = errors.New("bad")

func leakReported(s *S, bad bool) error {
	s.mu.Lock()
	if bad {
		return errBad // want "this return leaves s.mu locked while another path unlocks it: unlock before returning"
	}
	s.mu.Unlock()
	return nil
}

func leakReported2(s *S, bad bool) {
	s.mu.Lock()
	if bad {
		return // want "this return leaves s.mu locked while another path unlocks it: unlock before returning"
	}
	s.mu.Unlock()
}

func leakSilent(s *S, bad bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bad {
		return errBad
	}
	return nil
}

func leakSilent2(s *S) int {
	s.mu.Lock()
	s.mu.Unlock()
	return 1
}

func leakClosureSilent(s *S, n int) error {
	s.mu.Lock()
	if n == 0 {
		s.mu.Unlock()
		return nil
	}
	defer func() {
		s.mu.Unlock()
	}()
	if n == 1 {
		return errBad
	}
	return nil
}

func touchLeak(m *sync.Mutex) {
}

func tailUnknownSilent(s *S, n int) {
	s.mu.Lock()
	if n > 0 {
		s.mu.Unlock()
		return
	}
	touchLeak(&s.mu)
	s.mu.Unlock()
}

func endFreeReported(s *S, n int) {
	s.mu.Lock()
	if n > 0 {
		return // want "this return leaves s.mu locked while another path unlocks it: unlock before returning"
	}
	if n == 0 {
		s.mu.Unlock()
	} else {
		s.mu.Unlock()
	}
}

func leakLocalSilent(n int) error {
	var mu sync.Mutex
	mu.Lock()
	if n == 0 {
		mu.Unlock()
		return nil
	}
	return errBad
}

type embedS struct {
	sync.Mutex
}

func leakTextReported(s *embedS, bad bool) error { // the caller holds s.Mutex on entry
	if bad {
		s.Mutex.Unlock()
	}
	s.Lock()
	if bad {
		return errBad // want "this return leaves s locked while another path unlocks it: unlock before returning"
	}
	s.Unlock()
	return nil
}

type leakR struct {
	mu sync.RWMutex
}

func leakReadSilent(r *leakR, bad bool) {
	r.mu.RLock()
	if bad {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()
}

func leakHelperReleaseSilent(s *S, ok bool) error {
	s.mu.Lock()
	if !ok {
		return s.unlockWith(errBad)
	}
	s.mu.Unlock()
	return nil
}

func (s *S) unlockWith(err error) error {
	s.mu.Unlock()
	return err
}

func releaseForLeak(m *sync.Mutex, held bool) {
	if held {
		m.Unlock()
	}
}

func leakDeferAddrSilent(bad bool) error {
	var mu sync.Mutex
	mu.Lock()
	defer releaseForLeak(&mu, bad)
	if bad {
		return errBad
	}
	mu.Unlock()
	return nil
}

func leakStoredAddrSilent(bad bool) error {
	var mu sync.Mutex
	p := &mu
	_ = p
	mu.Lock()
	if bad {
		return errBad
	}
	mu.Unlock()
	return nil
}

func leakUnlockedReturnsSilent(s *S, n int) error {
	s.mu.Lock()
	if n > 0 {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return nil
}

func leakLockedOnlySilent(s *S, bad bool) error {
	s.mu.Lock()
	if bad {
		return errBad
	}
	s.mu.Unlock()
	s.mu.Lock()
	return errBad
}

func leakRelockSilent(s *S, n int) error {
	s.mu.Lock()
	if n == 0 {
		s.mu.Unlock()
		s.mu.Lock()
		return errBad
	}
	return errBad
}
