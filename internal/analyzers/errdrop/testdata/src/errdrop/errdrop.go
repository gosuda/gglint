package errdrop

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errdrop/dep"
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
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"text/template"
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

func outReportedTemplate(tmpl *template.Template, d string) string {
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

type WriteOnly interface {
	Write([]byte) (int, error)
}

func verdictReportedSingleWrite(w WriteOnly, b []byte) error {
	w.Write(b) // want "the error of w.Write is dropped and the next statement returns nil"
	return nil
}

type transcriptLike interface {
	Write([]byte) (int, error)
}

func verdictSilentSingleWrite(h hash.Hash) error {
	var w transcriptLike = h
	w.Write(nil)
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

func verdictSilentForwarder(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	var b [1]byte
	_, _ = f.Read(b[:])
	closeForwarder(f)
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
	return f.Name()
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
	defer f.Close() // want "is written in this function and the error of f.Close is dropped"
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

func outReportedFormatNode(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	if n == nil {
		return ""
	}
	format.Node(&buf, fset, n) // want "is read after the error of format.Node is dropped"
	return buf.String()
}

func outReportedStar(fset *token.FileSet, arg ast.Expr) string {
	var buf bytes.Buffer
	format.Node(&buf, fset, &ast.StarExpr{X: arg}) // want "is read after the error of format.Node is dropped"
	return buf.String()
}

func outReportedPrinterFprint(fset *token.FileSet, n ast.Node) string {
	var buf strings.Builder
	if n == nil {
		return ""
	}
	printer.Fprint(&buf, fset, n) // want "is read after the error of printer.Fprint is dropped"
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

func readInto(r io.Reader, p []byte) error {
	_, err := r.Read(p)
	return err
}

func outSilentReaderBound(p []byte) string {
	var buf bytes.Buffer
	readInto(&buf, p)
	return buf.String()
}

func durableSilentFailureBranch(p string, start func(io.Writer) error) (*os.File, error) {
	f, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	if err := start(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func durableReportedSuccessPath(p string, start func(io.Writer) error) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if err := start(f); err != nil {
		_ = f.Close()
		return err
	}
	_, err = f.Write([]byte{1})
	if err != nil {
		_ = f.Close()
		return err
	}
	defer f.Close() // want "is written in this function and the error of f.Close is dropped"
	return nil
}

type r1Sink struct{ f *os.File }

func (s r1Sink) Write(p []byte) (int, error) { return s.f.Write(p) }

func (s r1Sink) WriteString(x string) (int, error) { return len(x), nil }

func r1Log(f *os.File, msg string) error {
	fmt.Fprintf(r1Sink{f}, "%s\n", msg) // want "the error of fmt.Fprintf is dropped and the next statement returns nil"
	return nil
}

func r2Count(d []byte) (int, error) {
	if len(d) == 0 {
		return 0, errors.New("empty")
	}
	return len(d), nil
}

func r2Save(w io.Writer, d []byte) (int, error) { return r2Count(d) }

func r2Run(d []byte) error {
	var buf bytes.Buffer
	r2Save(&buf, d) // want "the error of r2Save is dropped and the next statement returns nil"
	return nil
}

func r3CloseIt() error { return errors.New("close failed") }

func r3Save(w io.Writer, d []byte) (err error) {
	defer func() {
		if cerr := r3CloseIt(); err == nil {
			err = cerr
		}
	}()
	_, err = w.Write(d)
	return err
}

func r3Run(d []byte) error {
	var buf bytes.Buffer
	r3Save(&buf, d) // want "the error of r3Save is dropped and the next statement returns nil"
	return nil
}

type outValidator interface{ Validate() error }

func (c *Config) Validate() error { return nil }

func outCheck(v outValidator) error { return v.Validate() }

func outSilentValidator() int {
	var c Config
	outCheck(&c)
	return c.Port
}

type outByteParser struct{ n int }

func (p *outByteParser) ReadByte() (byte, error) { return 0, nil }

func outDrain(r io.ByteReader) error {
	_, err := r.ReadByte()
	return err
}

func outSilentByteReader() int {
	var p outByteParser
	outDrain(&p)
	return p.n
}

func outSilentFieldDefault(b []byte) Config {
	var c Config
	c.Port = 8080
	json.Unmarshal(b, &c)
	return c
}

func newP(s string) (*int, error) { return nil, errors.New(s) }

func valueReportedStarDeref(s string) {
	p, _ := newP(s) // want "p is used after the error of newP is dropped"
	*p = 7
}

func outReportedFieldAfter(b []byte) Config {
	var c Config
	json.Unmarshal(b, &c) // want "c is read after the error of json.Unmarshal is dropped"
	c.Port = 80
	return c
}

type structVal struct{ V int }

func parseS(s string) (structVal, error) {
	if s == "" {
		return structVal{}, errors.New("empty")
	}
	return structVal{}, nil
}

func valueStructSilent(s string) int {
	v, _ := parseS(s)
	return v.V
}

func outValuedSpecSilent(b []byte) Config {
	var c Config = Config{Port: 1}
	json.Unmarshal(b, &c)
	return c
}

func closeFile(f *os.File) error { return f.Close() }

func durableSilentWriteInit(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return closeFile(f)
}

func durableSilentSyncUsed(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	defer f.Close()
	return f.Sync()
}

func outSilentFieldStoreOnly(b []byte) error {
	var c Config
	json.Unmarshal(b, &c)
	c.Port = 80
	return nil
}

func outReportedPointerFieldStore(b []byte) error {
	var p *Config
	json.Unmarshal(b, &p) // want "p is read after the error of json.Unmarshal is dropped"
	p.Port = 80
	return nil
}

type errdropInner struct {
	X int
}

type errdropOuter struct {
	In *errdropInner
}

func outReportedNestedPointerFieldStore(b []byte) error {
	var o errdropOuter
	json.Unmarshal(b, &o) // want "o is read after the error of json.Unmarshal is dropped"
	o.In.X = 1
	return nil
}

func outSilentReseatedPointer(b []byte) error {
	var o errdropOuter
	json.Unmarshal(b, &o)
	o.In = &errdropInner{}
	o.In.X = 1
	return nil
}

func outReportedWholeAfterPartialStore(b []byte) errdropOuter {
	var o errdropOuter
	json.Unmarshal(b, &o) // want "o is read after the error of json.Unmarshal is dropped"
	o.In = &errdropInner{}
	return o
}

func outReportedSelfRead(b []byte) error {
	var c Config
	json.Unmarshal(b, &c) // want "c is read after the error of json.Unmarshal is dropped"
	c.Port = c.Port + 1
	return nil
}

func outReportedReseatSelfRead(b []byte) error {
	var o errdropOuter
	json.Unmarshal(b, &o) // want "o is read after the error of json.Unmarshal is dropped"
	o.In = &errdropInner{X: o.In.X}
	return nil
}

func outReportedConditionalReseat(b []byte, bad bool) error {
	var o errdropOuter
	json.Unmarshal(b, &o) // want "o is read after the error of json.Unmarshal is dropped"
	if bad {
		o.In = &errdropInner{}
	}
	o.In.X = 1
	return nil
}

type noRecvT struct{ name string }

func (*noRecvT) Kind() string { return "t" }

func kindOfNoRecv(s string) (*noRecvT, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &noRecvT{}, nil
}

func valueSilentNoRecvTouch(s string) string {
	t, _ := kindOfNoRecv(s)
	return t.Kind()
}

type noTouchT struct{ name string }

func (t *noTouchT) Label() string { return "x" }

func loadNoTouch(s string) (*noTouchT, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &noTouchT{}, nil
}

func valueSilentNamedNoTouch(s string) string {
	t, _ := loadNoTouch(s)
	return t.Label()
}

type initDerefT struct{ n int }

func (t *initDerefT) Get() int {
	if n := t.n; t == nil {
		return n
	}
	return t.n
}

func loadInitDeref(s string) (*initDerefT, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &initDerefT{}, nil
}

func valueReportedGuardInitDeref(s string) int {
	t, _ := loadInitDeref(s) // want "t is used after the error of loadInitDeref is dropped"
	return t.Get()
}

type errdropSet map[string]struct{}

func (s *errdropSet) Len() int { return len(*s) }

func loadSet(p string) (errdropSet, error) {
	if p == "" {
		return nil, errors.New("empty")
	}
	return errdropSet{}, nil
}

func valueSilentMapMethod(p string) int {
	s, _ := loadSet(p)
	return s.Len()
}

func outSilentPrintf(x int) int {
	var n int
	fmt.Printf("n lives at %p\n", &n)
	return n + x
}

func outReportedSscanf(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n) // want "n is read after the error of fmt.Sscanf is dropped"
	return n
}

func durableSilentStoredUnused(p string) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	var w io.Writer = f
	_ = w
	return nil
}

func outSilentIncDecBefore(b []byte) Config {
	var c Config
	c.Port++
	json.Unmarshal(b, &c)
	return c
}

type indexConfig struct {
	Ports [2]int
}

func outSilentArrayIndexBefore(b []byte) indexConfig {
	var c indexConfig
	c.Ports[0] = 8080
	json.Unmarshal(b, &c)
	return c
}

type fallthroughT struct{ n int }

func (t *fallthroughT) Get() int {
	if t == nil {
		println("nil")
	}
	return t.n
}

func loadFallthrough(s string) (*fallthroughT, error) {
	if s == "" {
		return nil, errors.New("empty")
	}
	return &fallthroughT{}, nil
}

func valueReportedGuardFallthrough(s string) int {
	t, _ := loadFallthrough(s) // want "t is used after the error of loadFallthrough is dropped"
	return t.Get()
}

func outStoreThenReturnElse(b []byte, cond bool) (int, error) {
	var o errdropOuter
	json.Unmarshal(b, &o)
	if cond {
		o.In = &errdropInner{}
	} else {
		return 0, errors.New("no")
	}
	return o.In.X, nil
}

func outStoreThenPanicElse(b []byte, cond bool) int {
	var o errdropOuter
	json.Unmarshal(b, &o)
	if cond {
		o.In = &errdropInner{}
	} else {
		panic("no")
	}
	return o.In.X
}

func (s *errdropSet) Add(k string) { (*s)[k] = struct{}{} }

func valueMapWrite(p string) {
	s, _ := loadSet(p) // want "s is used after the error of loadSet is dropped"
	s.Add("x")
}

type errdropRows struct{ err error }

func (r errdropRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 0 {
		return errors.New("no dest")
	}
	n, ok := dest[0].(*int)
	if !ok {
		return errors.New("need *int")
	}
	*n = 42
	if len(dest) > 1 {
		return errors.New("one only")
	}
	return nil
}

func outReportedLocalScan(rows errdropRows) int {
	var n int
	rows.Scan(&n) // want "n is read after the error of rows.Scan is dropped"
	return n
}

type errdropKeyStore struct{}

func (errdropKeyStore) ScanKeys(dst ...any) error {
	if len(dst) == 0 {
		return errors.New("no dest")
	}
	return nil
}

func outSilentScanKeysStub(s errdropKeyStore) int {
	var n int
	s.ScanKeys(&n)
	return n
}

type errdropLazySet map[string]struct{}

func (s *errdropLazySet) Add(k string) {
	if *s == nil {
		*s = errdropLazySet{}
	}
	(*s)[k] = struct{}{}
}

func loadLazySet(p string) (errdropLazySet, error) {
	if p == "" {
		return nil, errors.New("empty")
	}
	return errdropLazySet{}, nil
}

func valueSilentLazyAlloc(p string) {
	s, _ := loadLazySet(p)
	s.Add("x")
}

func wrapInner(in *errdropInner) *errdropInner {
	if in == nil {
		return &errdropInner{}
	}
	return in
}

func outReportedWrapSelfRead(b []byte) error {
	var o errdropOuter
	json.Unmarshal(b, &o) // want "o is read after the error of json.Unmarshal is dropped"
	o.In = wrapInner(o.In)
	return nil
}

func Scan(s string, v fmt.Stringer, rest ...any) error {
	_ = rest
	if v == nil {
		return errors.New("nil")
	}
	return errors.New("no")
}

type errdropBoundedSet map[string]struct{}

func (s *errdropBoundedSet) Add(k string) {
	if len(*s) > 1000 {
		*s = errdropBoundedSet{}
	}
	(*s)[k] = struct{}{}
}

func loadBoundedSet(p string) (errdropBoundedSet, error) {
	if p == "" {
		return nil, errors.New("empty")
	}
	return errdropBoundedSet{}, nil
}

func valueBoundedWrite(p string) {
	s, _ := loadBoundedSet(p) // want "s is used after the error of loadBoundedSet is dropped"
	s.Add("x")
}

func outSilentScanFixedArg() string {
	var b bytes.Buffer
	Scan("a", &b)
	return b.String()
}

func durableReportedExecOut(cmd []string, outfile string) int {
	of, err := os.OpenFile(outfile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return -1
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	defer of.Close() // want "of is written in this function and the error of of.Close is dropped"
	c.Stdout = of
	c.Stderr = of
	if err := c.Run(); err != nil {
		return -1
	}
	return 0
}

type errdropWriterHolder struct {
	w io.Writer
}

func useWriterHolder(h errdropWriterHolder) {}

func durableSilentSamePackageField(p string) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	var h errdropWriterHolder
	h.w = f
	useWriterHolder(h)
	return nil
}

func durableSilentFieldNoUse(p string) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	var c errdropWriterHolder
	c.w = f
	return nil
}

type errdropTLSHolder struct {
	TLS *tls.Config
}

func durableReportedNestedFieldPassed(addr, p string) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close() // want "f is written in this function and the error of f.Close is dropped"
	var cfg errdropTLSHolder
	cfg.TLS = &tls.Config{}
	cfg.TLS.KeyLogWriter = f
	conn, err := tls.Dial("tcp", addr, cfg.TLS)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

type errdropDeadGuardSet map[string]struct{}

func (s *errdropDeadGuardSet) Add(k string) {
	if s == nil {
		*s = errdropDeadGuardSet{}
	}
	(*s)[k] = struct{}{}
}

func loadDeadGuardSet(p string) (errdropDeadGuardSet, error) {
	if p == "" {
		return nil, errors.New("empty")
	}
	return errdropDeadGuardSet{}, nil
}

func valueDeadGuardWrite(p string) {
	s, _ := loadDeadGuardSet(p) // want "s is used after the error of loadDeadGuardSet is dropped"
	s.Add("x")
}

func durableReportedSliceFieldPassed(addr, p string) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close() // want "f is written in this function and the error of f.Close is dropped"
	cfgs := []*tls.Config{{}}
	cfgs[0].KeyLogWriter = f
	conn, err := tls.Dial("tcp", addr, cfgs[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
