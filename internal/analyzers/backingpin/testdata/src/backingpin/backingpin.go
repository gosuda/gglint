package backingpin

type holder struct {
	b []byte
	s string
}

var (
	sinkBytes  []byte
	sinkString string
)

var table = map[string][]byte{}

var big [65537]byte

var head = big[:16] // want `subslice keeps a 65537-byte backing array alive: copy the bytes with slices.Clone`

func keep(b []byte) { sinkBytes = b }

func use(b []byte) int { return len(b) }

func identity(b []byte) []byte { return b }

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
	return buf[:10] // want `subslice keeps a 65537-byte backing array alive: copy the bytes with slices.Clone`
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
	return buf[:10] // want `subslice keeps a 65544-byte backing array alive`
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
	return b[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func makeCapAbove() []byte {
	b := make([]byte, 0, 65537)
	return b[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func makeShortWholeSlice() []byte {
	b := make([]byte, 1, 1<<20)
	return b[:] // want `subslice keeps a 1048576-byte backing array alive`
}

func makeShortWholeSliceLen() []byte {
	b := make([]byte, 1, 1<<20)
	return b[:len(b)] // want `subslice keeps a 1048576-byte backing array alive`
}

func makeZeroWholeSlice() []byte {
	b := make([]byte, 0, 1<<20)
	return b[:] // want `subslice keeps a 1048576-byte backing array alive`
}

func makeWholeSlice() []byte {
	b := make([]byte, 1<<20)
	return b[:]
}

func makeDynamicWholeSlice(n int) []byte {
	b := make([]byte, n, 1<<20)
	return b[:]
}

func makeWordsAt() []int32 {
	b := make([]int32, 16384)
	return b[:10]
}

func makeWordsAbove() []int32 {
	b := make([]int32, 16385)
	return b[:10] // want `subslice keeps a 65540-byte backing array alive`
}

func makeConstSize() []byte {
	const n = 1 << 17
	b := make([]byte, n)
	return b[:10] // want `subslice keeps a 131072-byte backing array alive`
}

func makeVarDecl() []byte {
	var b = make([]byte, 1<<17)
	return b[:10] // want `subslice keeps a 131072-byte backing array alive`
}

func makeOnceAssignedSize() []byte {
	n := 1 << 17
	b := make([]byte, n)
	return b[:10] // want `subslice keeps a 131072-byte backing array alive`
}

func makeIncrementedSize() []byte {
	n := 1 << 17
	n++
	b := make([]byte, n)
	return b[:10]
}

func newArrayAbove() []byte {
	p := new([65537]byte)
	return p[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func newArrayAt() []byte {
	p := new([65536]byte)
	return p[:10]
}

func arrayPointerParam(p *[65537]byte) []byte {
	return p[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func arrayPointerParamAt(p *[65536]byte) []byte {
	return p[:10]
}

func resliced() []byte {
	var buf [65537]byte
	b := buf[:]
	return b[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func reslicedAddressTaken() []byte {
	var buf [65537]byte
	b := buf[:]
	p := &b
	_ = p
	return b[:10]
}

func dynamicBound(n int) []byte {
	var buf [65537]byte
	return buf[:n] // want `subslice keeps a 65537-byte backing array alive`
}

func lowBound(n int) []byte {
	var buf [65537]byte
	return buf[n:] // want `subslice keeps a 65537-byte backing array alive`
}

func threeIndex() []byte {
	var buf [65537]byte
	return buf[:10:10] // want `subslice keeps a 65537-byte backing array alive`
}

func parenthesized() []byte {
	var buf [65537]byte
	return (buf[:10]) // want `subslice keeps a 65537-byte backing array alive`
}

func storedGlobal() {
	var buf [65537]byte
	sinkBytes = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func passedToLeakingParam() {
	var buf [65537]byte
	keep(buf[:10]) // want `subslice keeps a 65537-byte backing array alive`
}

func passedToNonLeakingParam() int {
	var buf [65537]byte
	return use(buf[:10])
}

func passedToReturningParam() int {
	var buf [65537]byte
	return len(identity(buf[:10]))
}

func returnedThroughIdentity() []byte {
	var buf [65537]byte
	return identity(buf[:10]) // want `subslice keeps a 65537-byte backing array alive`
}

func localSliceReturned() []byte {
	var buf [65537]byte
	s := buf[:10] // want `subslice keeps a 65537-byte backing array alive`
	return s
}

func localStores() {
	var a [70000]byte
	var x struct{ b []byte }
	x.b = a[:1]
	m := map[string][]byte{"k": a[2:3]}
	_ = m
}

func localCompositeUsed() int {
	var buf [65537]byte
	h := holder{b: buf[:10]}
	parts := [][]byte{buf[:10]}
	return len(h.b) + len(parts[0])
}

func localCompositeReturned() holder {
	var buf [65537]byte
	h := holder{b: buf[:10]} // want `subslice keeps a 65537-byte backing array alive`
	return h
}

func localFieldReturned() holder {
	var buf [65537]byte
	var h holder
	h.b = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
	return h
}

func localMapSent(ch chan map[string][]byte) {
	var buf [65537]byte
	m := make(map[string][]byte)
	m["k"] = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
	ch <- m
}

func storedParamMap(m map[string][]byte) {
	var buf [65537]byte
	m["k"] = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func storedParamSlice(parts [][]byte) {
	var buf [65537]byte
	parts[0] = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func storedThroughPointer(p *[]byte) {
	var buf [65537]byte
	*p = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
}

type pair[T any] struct{ a, b T }

func clip[T any]() []T {
	var a [70000]T
	return a[:1]
}

func clipPairs[T any]() []pair[T] {
	var a [70000]pair[T]
	return a[:1]
}

func clipMade[T any](n int) []T {
	b := make([]T, n, 70000)
	return b[:1]
}

func sentOnChannel(ch chan []byte) {
	var buf [65537]byte
	ch <- buf[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func storedInParameter(h *holder) {
	var buf [65537]byte
	h.b = buf[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func storedMap() {
	b := make([]byte, 65537)
	table["k"] = b[:10] // want `subslice keeps a 65537-byte backing array alive`
}

func structLiteral() holder {
	var buf [65537]byte
	return holder{b: buf[:10]} // want `subslice keeps a 65537-byte backing array alive`
}

func sliceLiteral() [][]byte {
	var buf [65537]byte
	parts := [][]byte{buf[:10]} // want `subslice keeps a 65537-byte backing array alive`
	return parts
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
	return s[:10] // want `substring keeps a 65537-byte backing string alive: copy it with strings\.Clone`
}

func substringOfMade() string {
	s := string(make([]byte, 1<<17))
	return s[:10] // want `substring keeps a 131072-byte backing string alive`
}

func substringOfMadeCapacity() string {
	s := string(make([]byte, 0, 1<<17))
	return s[:0]
}

func substringStored() {
	var buf [65537]byte
	s := string(buf[:])
	sinkString = s[2:12] // want `substring keeps a 65537-byte backing string alive`
}

func substringStoredInField(h *holder) {
	var buf [65537]byte
	s := string(buf[:])
	h.s = s[2:12] // want `substring keeps a 65537-byte backing string alive`
}

func substringOfBoundedConversion() string {
	var buf [65537]byte
	s := string(buf[:65537])
	return s[1:] // want `substring keeps a 65537-byte backing string alive`
}

func substringOfPartial() string {
	var buf [65537]byte
	s := string(buf[:100])
	return s[1:]
}

func substringOfConstant() string {
	const text = "constant strings live in read-only data"
	return text[1:]
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

func wholeWords() []int64 {
	var buf [8193]int64
	return buf[:8193]
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

func localAssign() int {
	var buf [65537]byte
	var s []byte
	s = buf[:10]
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

func localFieldReadReturned() []byte {
	var buf [1 << 20]byte
	var h struct{ b []byte }
	h.b = buf[:1] // want `subslice keeps a 1048576-byte backing array alive`
	return h.b
}

func localSiblingIntReturned() int {
	var buf [1 << 20]byte
	var h struct {
		b []byte
		n int
	}
	h.b = buf[:1]
	return h.n
}

func localSiblingSliceReturned(other []byte) []byte {
	var buf [1 << 20]byte
	var h struct{ b, c []byte }
	h.b = buf[:1]
	h.c = other
	return h.c
}

func localMapElemReturned() []byte {
	b := make([]byte, 1<<20)
	h := make(map[int][]byte)
	h[0] = b[:1] // want `subslice keeps a 1048576-byte backing array alive`
	return h[0]
}

func localArrayElemReturned() []byte {
	var buf [1 << 20]byte
	var a [2][]byte
	a[0] = buf[:1] // want `subslice keeps a 1048576-byte backing array alive`
	return a[0]
}

func localMapLenReturned() int {
	b := make([]byte, 1<<20)
	h := make(map[int][]byte)
	h[0] = b[:1]
	return len(h)
}

func localMapOtherKeyReturned() []byte {
	b := make([]byte, 1<<20)
	h := make(map[int][]byte)
	h[0] = b[:1]
	return h[1]
}

func localArrayUnknownIndexReturned(i int) []byte {
	var buf [1 << 20]byte
	var a [2][]byte
	a[0] = buf[:1] // want `subslice keeps a 1048576-byte backing array alive`
	return a[i]
}

func literalFieldReturned() []byte {
	var buf [1 << 20]byte
	h := struct{ b []byte }{buf[:1]} // want `subslice keeps a 1048576-byte backing array alive`
	return h.b
}

func literalKeyedFieldReturned() []byte {
	var buf [1 << 20]byte
	h := holder{b: buf[:1]} // want `subslice keeps a 1048576-byte backing array alive`
	return h.b
}

func literalSiblingFieldReturned() int {
	var buf [1 << 20]byte
	h := struct {
		b []byte
		n int
	}{b: buf[:1], n: 1}
	return h.n
}

func literalElemReturned() []byte {
	var buf [1 << 20]byte
	parts := [][]byte{buf[:1], nil} // want `subslice keeps a 1048576-byte backing array alive`
	return parts[0]
}

func literalOtherElemReturned() []byte {
	var buf [1 << 20]byte
	parts := [][]byte{buf[:1], nil}
	return parts[1]
}

func substringMapKeyReturned() map[string]int {
	s := string(make([]byte, 1<<17))
	return map[string]int{s[:1]: 1} // want `substring keeps a 131072-byte backing string alive`
}

func substringMapKeyBoundReturned() map[string]int {
	s := string(make([]byte, 1<<17))
	m := map[string]int{s[:1]: 1} // want `substring keeps a 131072-byte backing string alive`
	return m
}

func substringMapKeyLookup() int {
	s := string(make([]byte, 1<<17))
	m := map[string]int{s[:1]: 1}
	return m["x"]
}

func literalFieldAliasReturned() []byte {
	var buf [1 << 20]byte
	h := struct{ b []byte }{buf[:1]} // want `subslice keeps a 1048576-byte backing array alive`
	alias := h
	return alias.b
}

func literalSiblingFieldAliasReturned() int {
	var buf [1 << 20]byte
	h := struct {
		b []byte
		n int
	}{b: buf[:1], n: 1}
	var alias = h
	return alias.n
}

func literalElemAliasReturned() []byte {
	var buf [1 << 20]byte
	parts := [2][]byte{buf[:1], nil} // want `subslice keeps a 1048576-byte backing array alive`
	var alias [2][]byte
	alias = parts
	return alias[0]
}

func literalOtherElemAliasReturned() []byte {
	var buf [1 << 20]byte
	parts := [2][]byte{buf[:1], nil}
	alias := parts
	return alias[1]
}

func temporaryLiteralFieldReturned() []byte {
	var buf [1 << 20]byte
	return (struct{ b []byte }{buf[:1]}).b // want `subslice keeps a 1048576-byte backing array alive`
}

func temporaryLiteralSiblingFieldReturned() int {
	var buf [1 << 20]byte
	return (struct {
		b []byte
		n int
	}{b: buf[:1], n: 1}).n
}

func temporaryLiteralElemReturned() []byte {
	var buf [1 << 20]byte
	return [][]byte{buf[:1], nil}[0] // want `subslice keeps a 1048576-byte backing array alive`
}

func temporaryLiteralOtherElemReturned() []byte {
	var buf [1 << 20]byte
	return [][]byte{buf[:1], nil}[1]
}

func pointerLiteralFieldReturned() []byte {
	var buf [1 << 20]byte
	h := &struct{ b []byte }{buf[:1]} // want `subslice keeps a 1048576-byte backing array alive`
	return h.b
}

func derefFieldReturned() []byte {
	var buf [1 << 20]byte
	h := struct{ b []byte }{buf[:1]} // want `subslice keeps a 1048576-byte backing array alive`
	p := &h
	return (*p).b
}
