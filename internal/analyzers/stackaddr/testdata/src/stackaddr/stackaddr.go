package stackaddr

import "unsafe"

type holder struct {
	u uintptr
	n int
	p unsafe.Pointer
}

type addr uintptr

var (
	globalU   uintptr
	globalP   unsafe.Pointer
	globalH   holder
	globalInt int
	globalSet []uintptr
)

var literal = func() uintptr {
	x := 1
	return uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func use(uintptr) {}

func (h *holder) use(uintptr) {}

func keepLocal() {
	x := 1
	kept := uintptr(unsafe.Pointer(&x)) // want `^uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage as an integer the runtime cannot track: keep an unsafe\.Pointer$`
	use(kept)
}

func declLocal() {
	x := 1
	var kept = uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
	use(kept)
}

func storeGlobal() {
	var x int
	globalU = uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func compound() {
	var x int
	globalU += uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func returnParam(n int) uintptr {
	return uintptr(unsafe.Pointer(&n)) // want `uintptr\(unsafe\.Pointer\(&n\)\) keeps the address of stack storage`
}

func (h holder) returnReceiver() uintptr {
	return uintptr(unsafe.Pointer(&h)) // want `uintptr\(unsafe\.Pointer\(&h\)\) keeps the address of stack storage`
}

func namedResult() (r int) {
	globalU = uintptr(unsafe.Pointer(&r)) // want `uintptr\(unsafe\.Pointer\(&r\)\) keeps the address of stack storage`
	return r
}

func arithmetic() uintptr {
	var arr [4]int
	return uintptr(unsafe.Pointer(&arr)) + 8 // want `uintptr\(unsafe\.Pointer\(&arr\)\) keeps the address of stack storage`
}

func namedUintptr() addr {
	x := 1
	return addr(unsafe.Pointer(&x)) // want `addr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func parenthesized() {
	x := 1
	globalU = uintptr((unsafe.Pointer((&x)))) // want `uintptr\(\(unsafe\.Pointer\(\(&x\)\)\)\) keeps the address of stack storage`
}

func storedField(h *holder) {
	x := 1
	h.u = uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func compositeField() {
	x := 1
	h := holder{u: uintptr(unsafe.Pointer(&x))} // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
	_ = h
}

func compositeMapKey() map[uintptr]int {
	var x int
	return map[uintptr]int{uintptr(unsafe.Pointer(&x)): 1} // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func addressedCompositeElements() *struct{ addr uintptr } {
	var x int
	return &struct{ addr uintptr }{uintptr(unsafe.Pointer(&x))} // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func compositeFieldName() struct{ addr int } {
	var addr int
	_ = addr
	return struct{ addr int }{addr: 1}
}

func reconverted() addr {
	x := 1
	return addr(uintptr(unsafe.Pointer(&x))) // want `addr\(uintptr\(unsafe\.Pointer\(&x\)\)\) keeps the address of stack storage`
}

func sent(ch chan uintptr) {
	x := 1
	ch <- uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
}

func capturedInLiteral() {
	x := 1
	func() {
		globalU = uintptr(unsafe.Pointer(&x)) // want `uintptr\(unsafe\.Pointer\(&x\)\) keeps the address of stack storage`
	}()
}

func arrayElement() {
	var arr [4]int
	globalU = uintptr(unsafe.Pointer(&arr[2])) // want `uintptr\(unsafe\.Pointer\(&arr\[2\]\)\) keeps the address of stack storage`
}

func localField(h holder) uintptr {
	return uintptr(unsafe.Pointer(&h.n)) // want `uintptr\(unsafe\.Pointer\(&h\.n\)\) keeps the address of stack storage`
}

func newObject() uintptr {
	return uintptr(unsafe.Pointer(new(int))) // want `uintptr\(unsafe\.Pointer\(new\(int\)\)\) keeps the address of stack storage`
}

func compositeAddress() uintptr {
	return uintptr(unsafe.Pointer(&holder{})) // want `uintptr\(unsafe\.Pointer\(&holder\{\}\)\) keeps the address of stack storage`
}

func aliasTwice() {
	var x, y int
	p := &x
	p = &y
	globalU = uintptr(unsafe.Pointer(p)) // want `uintptr\(unsafe\.Pointer\(p\)\) keeps the address of stack storage`
}

func aliasMixed(other *int) uintptr {
	x := 1
	p := &x
	p = other
	return uintptr(unsafe.Pointer(p))
}

func rangeReassigned(ptrs []*int) uintptr {
	x := 1
	p := &x
	for _, p = range ptrs {
		_ = p
	}
	return uintptr(unsafe.Pointer(p))
}

func blank() {
	x := 1
	_ = uintptr(unsafe.Pointer(&x))
}

func compared() bool {
	x := 1
	return uintptr(unsafe.Pointer(&x)) == 0
}

func appended() {
	x := 1
	globalSet = append(globalSet, uintptr(unsafe.Pointer(&x)))
}

func directArgument() {
	x := 1
	use(uintptr(unsafe.Pointer(&x)))
}

func parenthesizedArgument() {
	x := 1
	use((uintptr(unsafe.Pointer(&x))))
}

func methodArgument(h *holder) {
	x := 1
	h.use(uintptr(unsafe.Pointer(&x)))
}

func goAndDeferArguments() {
	x := 1
	go use(uintptr(unsafe.Pointer(&x)))
	defer use(uintptr(unsafe.Pointer(&x)))
}

func globalVariable() uintptr {
	return uintptr(unsafe.Pointer(&globalInt))
}

func pointerParam(p *int) uintptr {
	return uintptr(unsafe.Pointer(p))
}

func fieldThroughPointer(h *holder) uintptr {
	return uintptr(unsafe.Pointer(&h.n))
}

func arrayThroughPointer(arr *[4]int) uintptr {
	return uintptr(unsafe.Pointer(&arr[1]))
}

func sliceElement(b []byte) uintptr {
	return uintptr(unsafe.Pointer(&b[0]))
}

func constant() uintptr {
	return uintptr(5)
}

func pointerReturned() unsafe.Pointer {
	x := 1
	return unsafe.Pointer(&x) // want `^unsafe\.Pointer\(&x\) takes the address of stack storage and stores it outside its frame$`
}

func pointerGlobal() {
	x := 1
	globalP = unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerGlobalField() {
	x := 1
	globalH.p = unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerThroughParam(h *holder) {
	x := 1
	h.p = unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerSent(ch chan unsafe.Pointer) {
	x := 1
	ch <- unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerNamedResult() (r unsafe.Pointer) {
	x := 1
	r = unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
	return
}

func pointerMapEntry(m map[int]unsafe.Pointer) {
	x := 1
	m[0] = unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerSliceElement(s []unsafe.Pointer) {
	x := 1
	s[0] = unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerComposite() holder {
	x := 1
	return holder{p: unsafe.Pointer(&x)} // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
}

func pointerRestored() unsafe.Pointer {
	var arr [4]int
	return unsafe.Pointer(uintptr(unsafe.Pointer(&arr[0])) + 8) // want `unsafe\.Pointer\(uintptr\(unsafe\.Pointer\(&arr\[0\]\)\) \+ 8\) takes the address of stack storage`
}

func pointerAliasReturned() unsafe.Pointer {
	x := 1
	p := unsafe.Pointer(&x) // want `unsafe\.Pointer\(&x\) takes the address of stack storage`
	return p
}

func pointerAliasChain() unsafe.Pointer {
	x := 1
	p := &x
	q := unsafe.Pointer(p) // want `unsafe\.Pointer\(p\) takes the address of stack storage`
	return q
}

func pointerLocal() int {
	x := 1
	p := unsafe.Pointer(&x)
	return *(*int)(p)
}

func pointerFieldInFrame() {
	var h holder
	x := 1
	h.p = unsafe.Pointer(&x)
	_ = h
}

func pointerRestoredLocal() int {
	var arr [4]int
	p := unsafe.Pointer(uintptr(unsafe.Pointer(&arr[0])) + 8)
	return *(*int)(p)
}

func pointerAliasReassigned(other unsafe.Pointer) unsafe.Pointer {
	x := 1
	p := unsafe.Pointer(&x)
	p = other
	return p
}

func plainPointer() *int {
	x := 1
	return &x
}
