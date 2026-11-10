package errdrop

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"text/template"

	"errdrop/dep"
)

type Config struct {
	Port int
}

type Tx struct{}

var errTx = errors.New("tx failed")

func (t *Tx) Commit() error {
	return errTx
}

func valueReported(p string) string {
	f, _ := os.Open(p) // want "is used after the error of os.Open is dropped"
	return f.Name()
}

func valueSilent(p string) string {
	f, _ := os.Open(p)
	if f == nil {
		return ""
	}
	return f.Name()
}

func valueSilent2(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func valueSilent3(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

func outReported(b []byte) Config {
	var c Config
	json.Unmarshal(b, &c) // want "is read after the error of json.Unmarshal is dropped"
	return c
}

func outReported2(tmpl *template.Template, d string) string {
	var buf bytes.Buffer
	tmpl.Execute(&buf, d) // want "is read after the error of tmpl.Execute is dropped"
	return buf.String()
}

func outSilent() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "x")
	return sb.String()
}

func outSilent2(b []byte) Config {
	var c Config
	_ = json.Unmarshal(b, &c)
	return c
}

func outSilent3(b []byte) Config {
	c := Config{Port: 1}
	json.Unmarshal(b, &c)
	return c
}

func durableReported(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close() // want "is written in this function and the error of f.Close is dropped"
	_, err = f.Write(b)
	return err
}

func durableSilent(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	b := make([]byte, 1)
	_, err = f.Read(b)
	return err
}

func durableSilent2(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err != nil {
		return err
	}
	return f.Close()
}

func durableSilent3(w io.WriteCloser) error {
	defer w.Close()
	return nil
}

func verdictReported(a, b string) error {
	os.Rename(a, b) // want "the error of os.Rename is dropped and the next statement returns nil"
	return nil
}

func verdictReported2(t *Tx) error {
	t.Commit() // want "the error of t.Commit is dropped and the next statement returns nil"
	return nil
}

func verdictSilent(h hash.Hash, b []byte) error {
	h.Write(b)
	return nil
}

func verdictSilent2(buf *bytes.Buffer, s string) error {
	buf.WriteString(s)
	return nil
}

func verdictSilent3(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	f.Close()
	return err
}

func verdictSilent4(p string) error {
	_ = os.Remove(p)
	return nil
}

func factNever() error {
	dep.Never()
	return nil
}

func factWrite() error {
	fmt.Fprintf(dep.T{}, "x")
	return nil
}

func pickRangeAssign(xs []error) (err error) {
	for _, err = range xs {
	}
	return
}

func verdictReported3(xs []error) error {
	pickRangeAssign(xs) // want "the error of pickRangeAssign is dropped and the next statement returns nil"
	return nil
}

func neverUnderscore() (_ error) { return }

func verdictSilent5() error {
	neverUnderscore()
	return nil
}

func decodeSecond(a, b any) error { return errors.New("no") }

func outReported3(p int) int {
	var c int
	decodeSecond(&p, &c) // want "c is read after the error of decodeSecond is dropped"
	return c
}

func outSilent4(b []byte, xs []Config) Config {
	var c Config
	json.Unmarshal(b, &c)
	for _, c = range xs {
	}
	return c
}

func durableSilent4(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(b)
	return f.Close()
}

type FieldWriter struct {
	Write func([]byte) (int, error)
}

func verdictReportedFieldWrite(s FieldWriter, b []byte) error {
	s.Write(b) // want "the error of s.Write is dropped and the next statement returns nil"
	return nil
}

func verdictReportedSyscallClose(fd int) error {
	syscall.Close(fd) // want "the error of syscall.Close is dropped and the next statement returns nil"
	return nil
}

type CloseFile struct{}

func (*CloseFile) Write(b []byte) (int, error) { return len(b), nil }

func (*CloseFile) Close(n *int) error { return errors.New("close failed") }

func outReportedCloseOut(n int, b []byte) int {
	var m int
	f := &CloseFile{}
	f.Write(b)
	f.Close(&m) // want "is read after the error of f.Close is dropped"
	return m + n
}

type writeOnly interface {
	Write([]byte) (int, error)
}

func verdictSilentSingleWrite(w writeOnly, b []byte) error {
	w.Write(b)
	return nil
}

func echoErr(err error) error { return err }

func verdictSilentEcho(err error) error {
	echoErr(err)
	return nil
}

var savedErr error

func recordErr(err error) error { savedErr = err; return savedErr }

func verdictSilentSink(err error) error {
	recordErr(err)
	return nil
}

func doWork() error { return errors.New("boom") }

func risky(err error) error {
	_ = err
	return doWork()
}

func verdictReportedSink(err error) error {
	risky(err) // want "the error of risky is dropped and the next statement returns nil"
	return nil
}

func computeErr() error { return errors.New("boom") }

func wrapCompute(err error) error {
	e := computeErr()
	_ = err
	return e
}

func verdictReportedComputed(err error) error {
	wrapCompute(err) // want "the error of wrapCompute is dropped and the next statement returns nil"
	return nil
}

func closeForwarder(c io.Closer) error {
	if c == nil {
		return nil
	}
	return c.Close()
}

func verdictSilentForwarder(c io.Closer) error {
	closeForwarder(c)
	return nil
}

func closeWithLog(c io.Closer) error {
	println("closing")
	return c.Close()
}

func verdictReportedForwarder(c io.Closer) error {
	closeWithLog(c) // want "the error of closeWithLog is dropped and the next statement returns nil"
	return nil
}

func verdictSilentDiscardAfterPeek(r *bufio.Reader) error {
	buf, err := r.Peek(2)
	if bytes.Equal(buf, []byte("\r\n")) {
		r.Discard(2)
		return nil
	}
	return err
}

func verdictReportedDiscard(r *bufio.Reader) error {
	r.Discard(2) // want "the error of r.Discard is dropped and the next statement returns nil"
	return nil
}

var errShort = errors.New("short")

func verdictReportedDiscardAfterRead(r *bufio.Reader, p []byte) error {
	buf, err := r.Peek(2)
	if err != nil {
		return err
	}
	_, _ = r.Read(p)
	_ = buf
	r.Discard(2) // want "the error of r.Discard is dropped and the next statement returns nil"
	return nil
}

type mismatchErr struct{}

func (*mismatchErr) Error() string { return "boom" }

func mismatchG() (*mismatchErr, error) { return &mismatchErr{}, nil }

func mismatchF() (error, error) { return mismatchG() }

func verdictReportedMismatch() error {
	mismatchF() // want "the error of mismatchF is dropped and the next statement returns nil"
	return nil
}

func setDefaults(c *Config) { c.Port = 80 }

func outSilentDefaults(b []byte) Config {
	var c Config
	setDefaults(&c)
	json.Unmarshal(b, &c)
	return c
}

func durableSilentReadOnly(p string) error {
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	b := make([]byte, 4)
	_, err = f.Read(b)
	return err
}

func durableSilentWriteTo(p string, w io.Writer) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteTo(w)
	return err
}

func valueReportedParen(p string) string {
	f, _ := os.Open(p) // want "is used after the error of os.Open is dropped"
	return (f).Name()
}

type nilNode struct {
	next *nilNode
}

func (n *nilNode) Len() int {
	if n == nil {
		return 0
	}
	return 1
}

func findNode(s string) (*nilNode, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &nilNode{}, nil
}

func valueSilentNilSafe(s string) int {
	n, _ := findNode(s)
	return n.Len()
}

func valueSilentNilSafeDep(s string) int {
	n, _ := dep.FindSafe(s)
	return n.Len()
}
