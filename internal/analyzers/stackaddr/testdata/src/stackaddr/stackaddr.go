package stackaddr

import "unsafe"

type holder struct {
	u uintptr
	n int
}

type addr uintptr

var (
	globalU   uintptr
	globalInt int
	globalSet []uintptr
)

func use(uintptr) {}

func (h *holder) use(uintptr) {}

func keepLocal() {
	x := 1
	kept := uintptr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr: the stack can move; convert inside the call expression`
	use(kept)
}

func storeGlobal() {
	var x int
	globalU = uintptr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr`
}

func returnParam(n int) uintptr {
	return uintptr(unsafe.Pointer(&n)) // want `address of stack variable n kept as uintptr`
}

func (h holder) returnReceiver() uintptr {
	return uintptr(unsafe.Pointer(&h)) // want `address of stack variable h kept as uintptr`
}

func namedResult() (r int) {
	globalU = uintptr(unsafe.Pointer(&r)) // want `address of stack variable r kept as uintptr`
	return r
}

func arithmetic() uintptr {
	var arr [4]int
	return uintptr(unsafe.Pointer(&arr)) + 8 // want `address of stack variable arr kept as uintptr`
}

func namedUintptr() addr {
	x := 1
	return addr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr`
}

func parenthesized() {
	x := 1
	globalU = uintptr((unsafe.Pointer((&x)))) // want `address of stack variable x kept as uintptr`
}

func storedField(h *holder) {
	x := 1
	h.u = uintptr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr`
}

func appended() {
	x := 1
	globalSet = append(globalSet, uintptr(unsafe.Pointer(&x))) // want `address of stack variable x kept as uintptr`
}

func reconverted() addr {
	x := 1
	return addr(uintptr(unsafe.Pointer(&x))) // want `address of stack variable x kept as uintptr`
}

func sent(ch chan uintptr) {
	x := 1
	ch <- uintptr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr`
}

func capturedInLiteral() {
	x := 1
	func() {
		globalU = uintptr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr`
	}()
}

func blank() {
	x := 1
	_ = uintptr(unsafe.Pointer(&x)) // want `address of stack variable x kept as uintptr`
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

func funcValueArgument() {
	x := 1
	f := use
	f(uintptr(unsafe.Pointer(&x)))
}

func goAndDeferArguments() {
	x := 1
	go use(uintptr(unsafe.Pointer(&x)))
	defer use(uintptr(unsafe.Pointer(&x)))
}

func globalVariable() uintptr {
	return uintptr(unsafe.Pointer(&globalInt))
}

func pointerKept() unsafe.Pointer {
	x := 1
	return unsafe.Pointer(&x)
}

func pointerParam(p *int) uintptr {
	return uintptr(unsafe.Pointer(p))
}

func fieldAddress(h holder) uintptr {
	return uintptr(unsafe.Pointer(&h.n))
}

func elementAddress(b []byte) uintptr {
	return uintptr(unsafe.Pointer(&b[0]))
}

func heapObject() uintptr {
	return uintptr(unsafe.Pointer(new(int)))
}

func constant() uintptr {
	return uintptr(5)
}
