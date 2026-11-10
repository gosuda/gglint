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
		return errBad // want "this return leaves s.mu locked while another return unlocks it"
	}
	s.mu.Unlock()
	return nil
}

func leakReported2(s *S, bad bool) {
	s.mu.Lock()
	if bad {
		return // want "this return leaves s.mu locked while another return unlocks it"
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
		return
	}
	touchLeak(&s.mu)
	s.mu.Unlock()
}

func endFreeReported(s *S, n int) {
	s.mu.Lock()
	if n > 0 {
		return // want "this return leaves s.mu locked while another return unlocks it"
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

func leakTextReported(s *embedS, bad bool) error {
	if bad {
		s.Mutex.Unlock()
	}
	s.Lock()
	if bad {
		return errBad // want "this return leaves s locked while another return unlocks it"
	}
	s.Unlock()
	return nil
}
