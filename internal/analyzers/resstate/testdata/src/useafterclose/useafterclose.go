package useafterclose

import (
	"fmt"
	"os"
)

func useaftercloseReported(p string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	f.Read(b) // want "is closed on every path to this call"
}

func useaftercloseSilent(p string, b []byte) {
	f, _ := os.Open(p)
	defer f.Close()
	f.Read(b)
}

func useaftercloseSilentEscape(p string, ch chan *os.File) *os.File {
	f, _ := os.Open(p)
	f.Close()
	_ = &f
	ch <- f
	g := f
	_ = g
	return f
}

type f1wrap struct {
	f *os.File
}

func (w f1wrap) Close() error {
	return w.f.Close()
}

func (w f1wrap) Name() string {
	return w.f.Name()
}

func useaftercloseSilentFieldRelease(p string) string {
	f, _ := os.Open(p)
	w := f1wrap{f: f}
	w.f.Close()
	return w.Name()
}

func useaftercloseSilentFieldUse(p string) string {
	f, _ := os.Open(p)
	w := f1wrap{f: f}
	w.Close()
	return w.f.Name()
}

func reopenAddr(p **os.File) {
	*p, _ = os.Open("x")
}

func useaftercloseSilentGoAddr(p string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	go reopenAddr(&f)
	_, _ = f.Read(b)
}

func useaftercloseSilentCapture(p, q string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	reopen := func() {
		f, _ = os.Open(q)
	}
	reopen()
	_, _ = f.Read(b)
}

func useaftercloseSilentRangeAssign(ch chan *os.File, b []byte) {
	f, _ := os.Open("x")
	f.Close()
	for f = range ch {
		_, _ = f.Read(b)
	}
}

func useaftercloseReportedLoop(p string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	for range 2 {
		_, _ = f.Read(b) // want "is closed on every path to this call"
	}
}

func useaftercloseReportedAfterCopy(p string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	name := f.Name()
	_, _ = f.Read(b) // want "is closed on every path to this call"
	_ = name
}

func useaftercloseSilentName(p string) string {
	f, _ := os.Open(p)
	f.Close()
	return f.Name()
}

func useaftercloseSilentDoubleClose(p string) {
	f, _ := os.Open(p)
	f.Close()
	_ = f.Close()
}

func useaftercloseReportedDerivedArg(p string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	fmt.Println(f.Name())
	_, _ = f.Read(b) // want "is closed on every path to this call"
}

type memBuf struct{ data []byte }

func (m *memBuf) Close() error { return nil }

func (m *memBuf) Write(p []byte) (int, error) {
	m.data = append(m.data, p...)
	return len(p), nil
}

func useaftercloseSilentForeignWrite(p []byte) {
	m := &memBuf{}
	m.Close()
	_, _ = m.Write(p)
}

type afterCloseProbe struct{}

func (afterCloseProbe) Close() error { return nil }

func (afterCloseProbe) Addr() string { return "" }

func (afterCloseProbe) LocalAddr() string { return "" }

func (afterCloseProbe) RemoteAddr() string { return "" }

func (afterCloseProbe) Err() error { return nil }

func (afterCloseProbe) ConnectionState() int { return 0 }

func (afterCloseProbe) WriteTo() int { return 0 }

func useaftercloseSilentAddr() string {
	p := afterCloseProbe{}
	p.Close()
	return p.Addr()
}

func useaftercloseSilentLocalAddr() string {
	p := afterCloseProbe{}
	p.Close()
	return p.LocalAddr()
}

func useaftercloseSilentRemoteAddr() string {
	p := afterCloseProbe{}
	p.Close()
	return p.RemoteAddr()
}

func useaftercloseSilentErr() error {
	p := afterCloseProbe{}
	p.Close()
	return p.Err()
}

func useaftercloseSilentConnectionState() int {
	p := afterCloseProbe{}
	p.Close()
	return p.ConnectionState()
}

func useaftercloseSilentWriteTo() int {
	p := afterCloseProbe{}
	p.Close()
	return p.WriteTo()
}

type sharedWrap struct {
	*os.File
}

func (s sharedWrap) Close() error { return nil }

func useaftercloseSilentWrapperClose(p string, b []byte) {
	f, _ := os.Open(p)
	s := sharedWrap{f}
	s.Close()
	_, _ = s.Write(b)
}
