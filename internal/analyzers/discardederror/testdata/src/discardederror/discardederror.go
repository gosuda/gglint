package discardederror

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

func f() error        { return nil }
func g() (int, error) { return 0, nil }
func h() int          { return 0 }

func statements() {
	f()   // want `error result of f\(\) is dropped; handle or propagate it`
	g()   // want `error result of g\(\) is dropped`
	(f()) // want `error result of f\(\) is dropped`
	func() {
		f() // want `error result of f\(\) is dropped`
	}()
}

func assignments() {
	_ = f()          // want `error result of f\(\) is dropped`
	_, _ = g()       // want `error result of g\(\) is dropped`
	n, _ := g()      // want `error result of g\(\) is dropped`
	_ = (f())        // want `error result of f\(\) is dropped`
	m, _ := h(), f() // want `error result of f\(\) is dropped`
	_, _ = f(), f()  // want `error result of f\(\) is dropped`
	_ = n + m
}

func notExempt(w io.Writer) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "x")        // want `error result of fmt\.Fprintf\(&b, "x"\) is dropped`
	fmt.Fprint(w, "x")          // want `error result of fmt\.Fprint\(w, "x"\) is dropped`
	fmt.Fprintln(os.Stdin, "x") // want `error result of fmt\.Fprintln\(os\.Stdin, "x"\) is dropped`
	_, _ = b.ReadByte()         // want `error result of b\.ReadByte\(\) is dropped`
	w.Write(nil)                // want `error result of w\.Write\(nil\) is dropped`
}

func exempt() {
	fmt.Print("x")
	fmt.Printf("%d", 1)
	fmt.Println("x")
	fmt.Fprint(os.Stdout, "x")
	fmt.Fprintf(os.Stderr, "x")
	fmt.Fprintln(os.Stdout, "x")
	(fmt.Println)("x")
	_, _ = fmt.Println("x")
	_, _ = fmt.Fprintln(os.Stderr, "x")
	var b bytes.Buffer
	b.WriteString("x")
	_, _ = b.Write(nil)
	var sb strings.Builder
	sb.WriteByte('x')
	_ = sb.WriteByte('x')
}

func hashWrites(p []byte, file *os.File, w io.Writer) {
	sha256.New().Write(p)
	var h hash.Hash = sha256.New()
	h.Write(p)
	_, _ = h.Write(p)
	var h32 hash.Hash32
	h32.Write(p)
	file.Write(p) // want `error result of file\.Write\(p\) is dropped`
	w.Write(p)    // want `error result of w\.Write\(p\) is dropped`
}

func handled() error {
	err := f()
	_ = err
	defer f()
	go f()
	n, err := g()
	_, _ = n, err
	h()
	return f()
}
