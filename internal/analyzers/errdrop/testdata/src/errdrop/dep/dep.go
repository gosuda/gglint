package dep

func Never() error {
	return nil
}

type T struct{}

func (T) Write(p []byte) (int, error) {
	return len(p), nil
}
