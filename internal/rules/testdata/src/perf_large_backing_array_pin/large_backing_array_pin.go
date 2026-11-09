// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/large_backing_array_pin_go.go.

// errorcheck -0

// perf::large_backing_array_pin on a Go package (R-U1): logos lint runs the
// rule inside escape analysis of Go code, so the Go twin of the loGo/s attack
// file reports the same slices. The limit is 65536 bytes: one byte below and
// exactly at it do not warn, one byte above does. A slice that covers the
// whole backing store is the allocation itself and does not warn.

package p

type holder struct {
	b []byte
	s string
}

var (
	sinkBytes  []byte
	sinkString string
)

func keep(b []byte) { sinkBytes = b }

func arrayBelow() []byte {
	var buf [65535]byte
	return buf[:10]
}

func arrayAt() []byte {
	var buf [65536]byte
	return buf[:10]
}

func arrayAbove() []byte {
	var buf [65537]byte
	return buf[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func wordsBelow() []int64 {
	var buf [8191]int64
	return buf[:10]
}

func wordsAt() []int64 {
	var buf [8192]int64
	return buf[:10]
}

func wordsAbove() []int64 {
	var buf [8193]int64
	return buf[:10] // want "subslice keeps a 65544-byte backing array alive"
}

func makeBelow() []byte {
	b := make([]byte, 65535)
	return b[:10]
}

func makeAt() []byte {
	b := make([]byte, 65536)
	return b[:10]
}

func makeAbove() []byte {
	b := make([]byte, 65537)
	return b[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func makeCapAbove() []byte {
	b := make([]byte, 0, 65537)
	return b[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func makeConstSize() []byte {
	const n = 1 << 17
	b := make([]byte, n)
	return b[:10] // want "subslice keeps a 131072-byte backing array alive"
}

func newArrayAbove() []byte {
	p := new([65537]byte)
	return p[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func newArrayAt() []byte {
	p := new([65536]byte)
	return p[:10]
}

func arrayPointerParam(p *[65537]byte) []byte {
	return p[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func arrayPointerParamAt(p *[65536]byte) []byte {
	return p[:10]
}

func resliced() []byte {
	var buf [65537]byte
	b := buf[:]
	return b[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func dynamicBound(n int) []byte {
	var buf [65537]byte
	return buf[:n] // want "subslice keeps a 65537-byte backing array alive"
}

func lowBound(n int) []byte {
	var buf [65537]byte
	return buf[n:] // want "subslice keeps a 65537-byte backing array alive"
}

func threeIndex() []byte {
	var buf [65537]byte
	return buf[:10:10] // want "subslice keeps a 65537-byte backing array alive"
}

func storedGlobal() {
	var buf [65537]byte
	sinkBytes = buf[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func passedToLeakingParam() {
	var buf [65537]byte
	keep(buf[:10]) // want "subslice keeps a 65537-byte backing array alive"
}

func sentOnChannel(ch chan []byte) {
	var buf [65537]byte
	ch <- buf[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func storedInParameter(h *holder) {
	var buf [65537]byte
	h.b = buf[:10] // want "subslice keeps a 65537-byte backing array alive"
}

func substringBelow() string {
	var buf [65535]byte
	s := string(buf[:])
	return s[:10]
}

func substringAt() string {
	var buf [65536]byte
	s := string(buf[:])
	return s[:10]
}

func substringAbove() string {
	var buf [65537]byte
	s := string(buf[:])
	return s[:10] // want "substring keeps a 65537-byte backing string alive"
}

func substringOfMade() string {
	s := string(make([]byte, 1<<17))
	return s[:10] // want "substring keeps a 131072-byte backing string alive"
}

func substringStored() {
	var buf [65537]byte
	s := string(buf[:])
	sinkString = s[2:12] // want "substring keeps a 65537-byte backing string alive"
}

func substringOfBoundedConversion() string {
	var buf [65537]byte
	s := string(buf[:65537])
	return s[1:] // want "substring keeps a 65537-byte backing string alive"
}

func substringOfPartial() string {
	var buf [65537]byte
	s := string(buf[:100])
	return s[1:]
}

func wholeArray() []byte {
	var buf [65537]byte
	return buf[:]
}

func wholeArrayZero() []byte {
	var buf [65537]byte
	return buf[0:]
}

func wholeArrayHigh() []byte {
	var buf [65537]byte
	return buf[0:65537]
}

func wholeArrayLen() []byte {
	var buf [65537]byte
	return buf[:len(buf)]
}

func wholeMade() []byte {
	b := make([]byte, 65537)
	return b[:len(b)]
}

func wholeString() string {
	var buf [65537]byte
	s := string(buf[:])
	return s[:]
}

func notEscaping() int {
	var buf [65537]byte
	s := buf[:10]
	return len(s)
}

func notEscapingMake() int {
	b := make([]byte, 65537)
	s := b[:10]
	return len(s)
}

func notEscapingString() int {
	var buf [65537]byte
	s := string(buf[:])
	return len(s[:10])
}

func smallLiteral() []byte {
	var buf [100]byte
	return buf[:10]
}

func unknownSize(b []byte, n int) []byte {
	return b[:n]
}

func unknownMake(n int) []byte {
	b := make([]byte, n)
	return b[:10]
}

func reassigned(n int) []byte {
	b := make([]byte, 65537)
	b = make([]byte, n)
	return b[:10]
}
