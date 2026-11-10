package errdrop

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"hash"
	"io"
	"log"
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

func outSilentTemplate(tmpl *template.Template, d string) string {
	var buf bytes.Buffer
	tmpl.Execute(&buf, d)
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
	defer f.Close() // want "is opened for writing in this function and the error of f.Close is dropped"
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
		_ = f.Close()
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
	_ = xs
	c = Config{}
	return c
}

func durableSilent4(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err != nil {
		_ = f.Close()
		return err
	}
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

func verdictReportedSingleWrite(w writeOnly, b []byte) error {
	w.Write(b) // want "the error of w.Write is dropped and the next statement returns nil"
	return nil
}

func echoErr(err error) error { return err }

func verdictSilentEcho(err error) error {
	echoErr(nil)
	return nil
}

var savedErr error

func recordErr(err error) error { savedErr = err; return savedErr }

func verdictSilentSink(err error) error {
	recordErr(nil)
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

func closeForwarder(c io.Closer) error {
	if c == nil {
		return nil
	}
	return c.Close()
}

func verdictSilentForwarder(c io.Closer) error {
	_ = closeForwarder(c)
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
	f, err := os.OpenFile(p, os.O_RDONLY, 0)
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
type nilRight struct{ n int }

func (t *nilRight) Len() int {
	if t.n == 0 || t == nil {
		return 0
	}
	return t.n
}

func parseRight(s string) (*nilRight, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &nilRight{}, nil
}

func valueReportedNilRightOr(s string) int {
	t, _ := parseRight(s) // want "is used after the error of parseRight is dropped"
	return t.Len()
}

type nilBody struct{ n int }

func (t *nilBody) Len() int {
	if t == nil {
		return t.n
	}
	return t.n
}

func parseBody(s string) (*nilBody, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &nilBody{}, nil
}

func valueReportedNilGuardBody(s string) int {
	t, _ := parseBody(s) // want "is used after the error of parseBody is dropped"
	return t.Len()
}

type innerSafe struct{}

func (i *innerSafe) Len() int {
	if i == nil {
		return 0
	}
	return 1
}

type outerPromoted struct{ *innerSafe }

func newOuterPromoted(s string) (*outerPromoted, error) { return nil, errors.New(s) }

func valueReportedPromoted(s string) int {
	o, _ := newOuterPromoted(s) // want "is used after the error of newOuterPromoted is dropped"
	return o.Len()
}

type recordFile struct{ err error }

func (f *recordFile) WriteRecord(b []byte) error { return nil }

func (f *recordFile) Close() error { return f.err }

func durableReportedWriteRecord(b []byte, err error) error {
	f := &recordFile{err: err}
	defer f.Close() // want "is opened for writing in this function and the error of f.Close is dropped"
	return f.WriteRecord(b)
}

func outReportedPrintfAddr(b []byte) Config {
	var c Config
	log.Printf("loading into %p", &c)
	json.Unmarshal(b, &c) // want "is read after the error of json.Unmarshal is dropped"
	return c
}

func writeAllHelper(f *os.File, b []byte) error {
	_, err := f.Write(b)
	return err
}

func durableSilentRdwrHelper(p string, b []byte) error {
	f, err := os.OpenFile(p, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	return errors.Join(writeAllHelper(f, b), f.Close())
}

type errdropValues map[string][]string

func (v errdropValues) Encode() string { return "" }

func parseErrdropValues(s string) (errdropValues, error) {
	if s == "" {
		return errdropValues{}, errors.New("empty")
	}
	return errdropValues{}, nil
}

func valueSilentValueRecv(s string) string {
	v, _ := parseErrdropValues(s)
	return v.Encode()
}

func verdictSilentHashIface(h hash.Hash, name string) error {
	io.WriteString(h, name)
	return nil
}

func outSilentCopyBuf(src *bytes.Buffer) []byte {
	var tmp bytes.Buffer
	io.Copy(&tmp, src)
	return tmp.Bytes()
}

func outSilentCopyBuilder(src *bytes.Buffer) string {
	var sb strings.Builder
	io.Copy(&sb, src)
	return sb.String()
}

func outSilentCopyN(src *bytes.Buffer) string {
	var sb strings.Builder
	io.Copy(&sb, io.LimitReader(src, 4))
	return sb.String()
}

type stickyPrinter struct{ err error }

func storeStickyByte(c byte) error { return nil }

func (w *stickyPrinter) WriteByte(c byte) error {
	if w.err == nil {
		w.err = storeStickyByte(c)
	}
	return w.err
}

func (w *stickyPrinter) Flush() error { return w.err }

func verdictSilentSticky(w *stickyPrinter) error {
	w.WriteByte('<')
	return nil
}

func verdictSilentStickyCheck(w *stickyPrinter) error {
	if err := verdictSilentSticky(w); err != nil {
		return err
	}
	return w.Flush()
}

func verdictSilentUnreadByte(r io.ByteScanner) (int, error) {
	ch, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if ch != 'e' {
		r.UnreadByte()
		return 0, nil
	}
	return 1, nil
}

func verdictSilentUnreadRune(r io.RuneScanner) (int, error) {
	ch, _, err := r.ReadRune()
	if err != nil {
		return 0, err
	}
	if ch != 'e' {
		r.UnreadRune()
		return 0, nil
	}
	return 1, nil
}

func durableSilentRdwrDecode(p string, v any) error {
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

var errdropGlobalOff bool

type lorGuard struct{ n int }

func (t *lorGuard) Len() int {
	if errdropGlobalOff || t == nil {
		return 0
	}
	return t.n
}

func parseLorGuard(s string) (*lorGuard, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &lorGuard{}, nil
}

func valueSilentLorGuard(s string) int {
	t, _ := parseLorGuard(s)
	return t.Len()
}

type passGuard struct{ n int }

func (t *passGuard) Len() int {
	if t == nil {
		log.Print(t)
		return 0
	}
	return t.n
}

func parsePassGuard(s string) (*passGuard, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &passGuard{}, nil
}

func valueSilentBodyPass(s string) int {
	t, _ := parsePassGuard(s)
	return t.Len()
}

func outSilentUnmarshalBefore(prefix, b []byte) Config {
	var c Config
	_ = json.Unmarshal(prefix, &c)
	if json.Unmarshal(b, &c) != nil {
		return Config{}
	}
	return c
}

func (c *Config) resetPort() { c.Port = 8080 }

func outSilentResetBefore(b []byte) Config {
	var c Config
	c.resetPort()
	if json.Unmarshal(b, &c) != nil {
		return Config{Port: 8080}
	}
	return c
}

type errdropJournal struct{ err error }

func (j *errdropJournal) Close() error { return j.err }

func (j *errdropJournal) WriteEnabled() bool { return true }

func durableSilentGetter() bool {
	j := &errdropJournal{}
	defer j.Close()
	return j.WriteEnabled()
}
type errdropWriter interface {
	WriteString(string) (int, error)
}

func errdropPut(w errdropWriter, s string) error {
	_, err := w.WriteString(s)
	return err
}

func outSilentPutWriter(s string) string {
	var buf bytes.Buffer
	errdropPut(&buf, s)
	return buf.String()
}

func outSilentEscapeText(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func outSilentWriteTo(src *bytes.Buffer) []byte {
	var out bytes.Buffer
	src.WriteTo(&out)
	return out.Bytes()
}

func outSilentFormatNode(fset *token.FileSet, d ast.Decl) string {
	var buf bytes.Buffer
	if d == nil {
		return ""
	}
	format.Node(&buf, fset, &ast.BasicLit{Kind: token.INT, Value: "1"})
	_ = d
	return buf.String()
}

func outSilentStar(fset *token.FileSet, arg ast.Expr) string {
	var buf bytes.Buffer
	format.Node(&buf, fset, &ast.StarExpr{X: arg})
	return buf.String()
}

func outSilentPrinterFprint(fset *token.FileSet, n ast.Node) string {
	var buf strings.Builder
	if n == nil {
		return ""
	}
	printer.Fprint(&buf, fset, &ast.BasicLit{Kind: token.INT, Value: "1"})
	_ = n
	return buf.String()
}

var errdropReplacer = strings.NewReplacer("\n", "\\n")

func outSilentReplacer(s string) string {
	var buf strings.Builder
	errdropReplacer.WriteString(&buf, s)
	return buf.String()
}

func durableSilentCreateNoWrite() (string, error) {
	f, err := os.CreateTemp("", "scratch")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	_ = os.Remove(path)
	return path, nil
}

func durableSilentTLS(addr string, cfg *tls.Config) error {
	c, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = io.WriteString(c, "hello")
	return err
}

type errdropPromotedPrinter struct {
	*bufio.Writer
}

func verdictSilentPromoted(p *errdropPromotedPrinter) error {
	p.WriteByte('<')
	return nil
}

func verdictSilentPromotedCaller(w *bufio.Writer) error {
	p := &errdropPromotedPrinter{w}
	if err := verdictSilentPromoted(p); err != nil {
		return err
	}
	return p.Flush()
}

func verdictSilentPromotedFlush(w *bufio.Writer) error {
	p := &errdropPromotedPrinter{w}
	p.WriteByte('<')
	return p.Flush()
}

type wireOut struct {
	Name string
}

func (w *wireOut) Write(p []byte) (int, error) { return len(p), nil }

func outReportedWire(data []byte) string {
	var w wireOut
	json.Unmarshal(data, &w) // want "is read after the error of json.Unmarshal is dropped"
	return w.Name
}
