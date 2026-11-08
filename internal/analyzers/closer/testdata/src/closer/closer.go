package closer

import (
	"bufio"
	"io"
	"os"
)

type wrapper struct{ f *os.File }

type conn struct{ open bool }

func (c *conn) Close() error {
	c.open = false
	return nil
}

func dial() conn { return conn{open: true} }

type quiet struct{}

func (quiet) Close() {}

func newQuiet() quiet { return quiet{} }

var registry = map[string]*os.File{}

func consume(*os.File) {}

func later(func()) {}

func mustOpen() *os.File {
	f, err := os.Open("x")
	if err != nil {
		panic(err)
	}
	return f
}

func openPair() error {
	f, err := os.Open("x") // want `f is never closed: defer f.Close\(\) or return it`
	if err != nil {
		return err
	}
	_ = f.Name()
	return nil
}

func create() {
	f, err := os.Create("x") // want `f is never closed: defer f.Close\(\) or return it`
	if err != nil {
		return
	}
	_, _ = f.Write(nil)
}

func closedWithoutDefer() {
	f := mustOpen() // want `f is never closed`
	if err := f.Close(); err != nil {
		panic(err)
	}
}

func blankDiscard() {
	f := mustOpen() // want `f is never closed`
	_ = f
}

func blankPairDiscard() {
	f, err := os.Open("x") // want `f is never closed`
	if err != nil {
		panic(err)
	}
	_, _ = err, f
}

func compareOnly() {
	f := mustOpen() // want `f is never closed`
	if f != nil {
		_ = f.Name()
	}
}

func pointerReceiverClose() {
	c := dial() // want `c is never closed: defer c.Close\(\) or return it`
	_ = c.open
}

func reassigned() {
	f := mustOpen() // want `f is never closed`
	f = mustOpen()
	_ = f.Name()
}

func shadowed() {
	f := mustOpen()
	defer f.Close()
	{
		f := mustOpen() // want `f is never closed`
		_ = f.Name()
	}
}

func nopCloser(r io.Reader) {
	c := io.NopCloser(r) // want `c is never closed`
	if c == nil {
		panic("nil")
	}
}

func laterAssign() {
	var c io.Closer
	var err error
	c, err = os.Open("x") // want `c is never closed`
	if c == nil || err != nil {
		panic("open")
	}
}

func declWithValue() {
	var c io.Closer = mustOpen() // want `c is never closed`
	_ = c
}

func readerFromCloser() {
	var r io.Reader = mustOpen() // want `r is never closed`
	_ = r
}

func parenthesized() {
	f := (mustOpen()) // want `f is never closed`
	_ = (f).Name()
}

func inLiteral() {
	func() {
		f := mustOpen() // want `f is never closed`
		_ = f.Name()
	}()
}

func deferred() {
	f := mustOpen()
	defer f.Close()
	_ = f.Name()
}

func deferredInBranch(ok bool) {
	f := mustOpen()
	if ok {
		defer f.Close()
	}
	_ = f.Name()
}

func deferredLiteral() {
	f := mustOpen()
	defer func() {
		if err := f.Close(); err != nil {
			panic(err)
		}
	}()
	_ = f.Name()
}

func deferredPointerReceiver() {
	c := dial()
	defer c.Close()
	_ = c.open
}

func returned() *os.File {
	f := mustOpen()
	return f
}

func returnedPair() (*os.File, error) {
	f, err := os.Open("x")
	return f, err
}

func wrapped() *wrapper {
	f := mustOpen()
	return &wrapper{f}
}

func storedField(w *wrapper) {
	f := mustOpen()
	w.f = f
}

func storedMap() {
	f := mustOpen()
	registry["k"] = f
}

func storedSlice() []*os.File {
	f := mustOpen()
	list := []*os.File{f}
	return list
}

func sent() {
	f := mustOpen()
	ch := make(chan *os.File, 1)
	ch <- f
}

func passedOn() {
	f := mustOpen()
	consume(f)
}

func discardedCall() {
	f := mustOpen()
	_ = bufio.NewReader(f)
}

func captured() func() error {
	f := mustOpen()
	return func() error { return f.Close() }
}

func capturedRead() {
	f := mustOpen()
	later(func() { _ = f.Name() })
}

func methodValue() func() error {
	f := mustOpen()
	cleanup := f.Close
	return cleanup
}

func aliased() *os.File {
	f := mustOpen()
	g := f
	return g
}

func namedResult() (f *os.File, err error) {
	f, err = os.Open("x")
	return
}

func namedResultReassigned() (f *os.File) {
	f = mustOpen()
	_ = f.Name()
	return nil
}

func paramReassigned(f *os.File) {
	f = mustOpen()
	_ = f.Name()
}

func assignedInLiteral() {
	var f *os.File
	func() {
		f = mustOpen()
	}()
	_ = f.Name()
}

func closeWithoutError() {
	q := newQuiet()
	_ = q
}

func notFromCall() {
	g := os.Stdout
	h := &os.File{}
	_, _ = g, h
}

func conversion(f *os.File) {
	c := io.Closer(f)
	_ = c
}

func builtin() {
	h := new(os.File)
	_ = h
}
