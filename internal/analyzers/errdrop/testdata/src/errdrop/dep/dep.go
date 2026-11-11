package dep

import (
	"errors"
	"io"
)

func Never() error {
	return nil
}

type T struct{}

func (T) Write(p []byte) (int, error) {
	return len(p), nil
}

type HashLike struct{}

func (HashLike) Write(p []byte) (int, error) { return 0, errors.New("short") }
func (HashLike) Sum(b []byte) []byte         { return b }
func (HashLike) Reset()                      {}
func (HashLike) Size() int                   { return 0 }
func (HashLike) BlockSize() int              { return 0 }

func FlushHashLike(b []byte) error {
	var h HashLike
	h.Write(b)
	return nil
}

type Safe struct {
	n int
}

func (s *Safe) Len() int {
	if s == nil || s.n == 0 {
		return 0
	}
	return s.n
}

func FindSafe(s string) (*Safe, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &Safe{}, nil
}

type Failure struct{}

func (Failure) Error() string { return "dep failed" }

func SaveFailure(w io.Writer) error {
	return Failure{}
}
