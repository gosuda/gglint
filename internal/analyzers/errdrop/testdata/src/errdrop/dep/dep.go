package dep

import "errors"

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
