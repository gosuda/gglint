package backingpin

type holder struct {
	b []byte
}

var sink []byte

var table = map[string][]byte{}

var big [65536]byte

var head = big[:16] // want `subslice keeps a 65536-byte backing array alive: copy the bytes with slices.Clone`

func keep(b []byte) { sink = b }

func arrayBelow() []byte {
	var buf [65535]byte
	return buf[:10]
}

func arrayAt() []byte {
	var buf [65536]byte
	return buf[:10] // want `subslice keeps a 65536-byte backing array alive: copy the bytes with slices.Clone`
}

func wordsBelow() []int64 {
	var buf [8191]int64
	return buf[:10]
}

func wordsAt() []int64 {
	var buf [8192]int64
	return buf[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func makeBelow() []byte {
	b := make([]byte, 65535)
	return b[:10]
}

func makeAt() []byte {
	b := make([]byte, 65536)
	return b[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func makeCapacity() []byte {
	b := make([]byte, 0, 65536)
	return b[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func makeWordsBelow() []int32 {
	b := make([]int32, 16383)
	return b[:10]
}

func makeWordsAt() []int32 {
	b := make([]int32, 16384)
	return b[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func makeConstant() []byte {
	const n = 1 << 17
	var b = make([]byte, n)
	return b[:10] // want `subslice keeps a 131072-byte backing array alive`
}

func pointerToArray(p *[65536]byte) []byte {
	return p[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func dynamicBounds(n int) []byte {
	var buf [65536]byte
	return buf[n:] // want `subslice keeps a 65536-byte backing array alive`
}

func threeIndex() []byte {
	var buf [65536]byte
	return buf[:10:10] // want `subslice keeps a 65536-byte backing array alive`
}

func parenthesized() []byte {
	var buf [65536]byte
	return (buf[:10]) // want `subslice keeps a 65536-byte backing array alive`
}

func storedGlobal() {
	var buf [65536]byte
	sink = buf[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func storedField(h *holder) {
	var buf [65536]byte
	h.b = buf[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func storedMap() {
	b := make([]byte, 65536)
	table["k"] = b[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func structLiteral() holder {
	var buf [65536]byte
	return holder{b: buf[:10]} // want `subslice keeps a 65536-byte backing array alive`
}

func sliceLiteral() [][]byte {
	var buf [65536]byte
	parts := [][]byte{buf[:10]} // want `subslice keeps a 65536-byte backing array alive`
	return parts
}

func sentOnChannel(ch chan<- []byte) {
	var buf [65536]byte
	ch <- buf[:10] // want `subslice keeps a 65536-byte backing array alive`
}

func wholeArray() []byte {
	var buf [65536]byte
	return buf[:] // want `subslice keeps a 65536-byte backing array alive`
}

func wholeArrayFromZero() []byte {
	var buf [65536]byte
	return buf[0:] // want `subslice keeps a 65536-byte backing array alive`
}

func localOnly() int {
	var buf [65536]byte
	s := buf[:10]
	return len(s)
}

func localAssign() int {
	var buf [65536]byte
	var s []byte
	s = buf[:10]
	return len(s)
}

func passedToCall() {
	var buf [65536]byte
	keep(buf[:10])
}

func reassigned(n int) []byte {
	b := make([]byte, 65536)
	b = make([]byte, n)
	return b[:10]
}

func dynamicMake(n int) []byte {
	b := make([]byte, n)
	return b[:10]
}

func parameterSlice(b []byte) []byte {
	return b[:10]
}

func resliced() []byte {
	var buf [65536]byte
	b := buf[:]
	return b[:10]
}
