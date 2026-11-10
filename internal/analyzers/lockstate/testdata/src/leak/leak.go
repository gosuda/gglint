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
