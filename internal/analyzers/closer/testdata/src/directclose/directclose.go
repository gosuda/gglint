package directclose

import "io"

type resource struct{}

func (resource) Close() error { return nil }
func openResource() resource { return resource{} }

func assignment() {
	f := openResource()
	err := f.Close()
	_ = err
}

func discarded() {
	f := openResource()
	_ = f.Close()
}

func declaration() {
	f := openResource()
	var err = f.Close()
	_ = err
}

func parenthesized() {
	f := openResource()
	err := (f.Close())
	_ = err
}

func parallelAssignment() {
	f := openResource()
	err, n := f.Close(), 1
	_, _ = err, n
}

func parallelDeclaration() {
	f := openResource()
	var err, n = f.Close(), 1
	_, _ = err, n
}

func ifInitializer() {
	f := openResource()
	if err := f.Close(); err != nil {
		panic(err)
	}
}

func forInitializer() {
	f := openResource()
	for err := f.Close(); err != nil; {
		break
	}
}

func switchInitializer() {
	f := openResource()
	switch err := f.Close(); err {
	default:
	}
}

func statement() {
	f := openResource()
	f.Close()
}

func returned() error {
	f := openResource()
	return f.Close()
}

func stableAlias() {
	f := openResource()
	g := f
	err := g.Close()
	_ = err
}

func noClose() {
	f := openResource() // want `f is never closed`
	_ = f
}

func conditionalAnd(cond bool) {
	f := openResource() // want `f is never closed`
	_ = cond && f.Close() == nil
}

func conditionalOr(cond bool) {
	f := openResource() // want `f is never closed`
	_ = cond || f.Close() == nil
}

func differentObject() {
	f := openResource() // want `f is never closed`
	g := openResource()
	_ = g.Close()
	_ = f
}

func shadowedName() {
	f := openResource() // want `f is never closed`
	{
		f := openResource()
		_ = f.Close()
	}
	_ = f
}

func asynchronous() {
	f := openResource() // want `f is never closed`
	go f.Close()
}

func noOpCloser(r io.Reader) {
	f := io.NopCloser(r)
	_ = f
}

func NopCloser() resource { return resource{} }

func localNoOpSpelling() {
	f := NopCloser() // want `f is never closed`
	_ = f
}
