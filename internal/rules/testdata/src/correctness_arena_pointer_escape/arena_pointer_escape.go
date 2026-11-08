// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/arena_pointer_escape_go.go.

// errorcheck

package p

import "unsafe"

type holder struct {
	p unsafe.Pointer
	u uintptr
}

type addr uintptr

var (
	globalU uintptr
	globalA addr
	globalP unsafe.Pointer
	globalH holder
)

func use(uintptr) {}

func keepU() {
	var x int
	globalU = uintptr(unsafe.Pointer(&x)) // want "address of stack"
}

func localU() {
	var x int
	a := uintptr(unsafe.Pointer(&x)) // want "address of stack"
	_ = a
}

func retU() uintptr {
	var x int
	return uintptr(unsafe.Pointer(&x)) // want "address of stack"
}

func arithU() {
	var x int
	a := uintptr(unsafe.Pointer(&x)) + 8 // want "address of stack"
	_ = a
}

func maskU() {
	var x int
	globalU = uintptr(unsafe.Pointer(&x)) &^ 7 // want "address of stack"
}

func litU() {
	var x int
	h := holder{u: uintptr(unsafe.Pointer(&x))} // want "address of stack"
	_ = h
}

func fieldU() {
	var h holder
	var x int
	h.u = uintptr(unsafe.Pointer(&x)) // want "address of stack"
	_ = h
}

func arrayElemU() {
	var arr [4]int
	globalU = uintptr(unsafe.Pointer(&arr[2])) // want "address of stack"
}

func structFieldU() {
	var s holder
	globalU = uintptr(unsafe.Pointer(&s.u)) // want "address of stack"
}

func newU() {
	globalU = uintptr(unsafe.Pointer(new(int))) // want "address of stack"
}

func litAddrU() {
	globalU = uintptr(unsafe.Pointer(&holder{})) // want "address of stack"
}

func aliasU() {
	var x int
	p := &x
	globalU = uintptr(unsafe.Pointer(p)) // want "address of stack"
}

func aliasTwiceU() {
	var x, y int
	p := &x
	p = &y
	globalU = uintptr(unsafe.Pointer(p)) // want "address of stack"
}

func paramU(n int) uintptr {
	return uintptr(unsafe.Pointer(&n)) // want "address of stack"
}

func sendU() {
	var x int
	ch := make(chan uintptr, 1)
	ch <- uintptr(unsafe.Pointer(&x)) // want "address of stack"
}

func declU() {
	var x int
	var a = uintptr(unsafe.Pointer(&x)) // want "address of stack"
	_ = a
}

func closureU() {
	var x int
	func() {
		globalU = uintptr(unsafe.Pointer(&x)) // want "address of stack"
	}()
}

func pairU() {
	var x int
	a, b := uintptr(unsafe.Pointer(&x)), 1 // want "address of stack"
	_, _ = a, b
}

func namedTypeU() {
	var x int
	globalA = addr(unsafe.Pointer(&x)) // want "address of stack"
}

func retP() unsafe.Pointer {
	var x int
	return unsafe.Pointer(&x) // want "address of stack"
}

func keepP() {
	var x int
	globalP = unsafe.Pointer(&x) // want "address of stack"
}

func viaParamP(h *holder) {
	var x int
	h.p = unsafe.Pointer(&x) // want "address of stack"
}

func globalFieldP() {
	var x int
	globalH.p = unsafe.Pointer(&x) // want "address of stack"
}

func sendP() {
	var x int
	ch := make(chan unsafe.Pointer, 1)
	ch <- unsafe.Pointer(&x) // want "address of stack"
}

func arrayElemP() unsafe.Pointer {
	var arr [4]int
	return unsafe.Pointer(&arr[1]) // want "address of stack"
}

func newP() unsafe.Pointer {
	return unsafe.Pointer(new(int)) // want "address of stack"
}

func litAddrP() unsafe.Pointer {
	return unsafe.Pointer(&holder{}) // want "address of stack"
}

func aliasP() unsafe.Pointer {
	var x int
	p := &x
	return unsafe.Pointer(p) // want "address of stack"
}

func namedResultP() (r unsafe.Pointer) {
	var x int
	r = unsafe.Pointer(&x) // want "address of stack"
	return
}

func mapStoreP() {
	var x int
	m := map[int]unsafe.Pointer{}
	m[0] = unsafe.Pointer(&x) // want "address of stack"
}

func sliceStoreP() {
	var x int
	s := make([]unsafe.Pointer, 1)
	s[0] = unsafe.Pointer(&x) // want "address of stack"
}

func litRetP() holder {
	var x int
	return holder{p: unsafe.Pointer(&x)} // want "address of stack"
}

func restoredP() unsafe.Pointer {
	var arr [4]int
	return unsafe.Pointer(uintptr(unsafe.Pointer(&arr[0])) + 8) // want "address of stack"
}

func localP() int {
	var x int
	p := unsafe.Pointer(&x)
	return *(*int)(p)
}

func fieldInFrameP() {
	var h holder
	var x int
	h.p = unsafe.Pointer(&x)
	_ = h
}

func restoredLocalP() int {
	var arr [4]int
	p := unsafe.Pointer(uintptr(unsafe.Pointer(&arr[0])) + 8)
	return *(*int)(p)
}

func argumentU() {
	var x int
	use(uintptr(unsafe.Pointer(&x)))
}

func compareU() bool {
	var x int
	return uintptr(unsafe.Pointer(&x)) == 0
}

func blankU() {
	var x int
	_ = uintptr(unsafe.Pointer(&x))
}

func paramPointerU(p *int) uintptr {
	return uintptr(unsafe.Pointer(p))
}

func globalObjectU() uintptr {
	return uintptr(unsafe.Pointer(&globalU))
}

func heapSliceU(b []byte) uintptr {
	return uintptr(unsafe.Pointer(&b[0]))
}

func viaPointerU(h *holder) uintptr {
	return uintptr(unsafe.Pointer(&h.u))
}

func aliasMixedU(other *int) uintptr {
	var x int
	p := &x
	p = other
	return uintptr(unsafe.Pointer(p))
}

func plainPointer() *int {
	var x int
	return &x
}

func aliasRetP() unsafe.Pointer {
	var x int
	p := unsafe.Pointer(&x) // want "address of stack"
	return p
}

func aliasChainP() unsafe.Pointer {
	var x int
	p := &x
	q := unsafe.Pointer(p) // want "address of stack"
	return q
}

func aliasGlobalP() {
	var x int
	p := unsafe.Pointer(&x) // want "address of stack"
	globalP = p
}

func aliasReassignedP(other unsafe.Pointer) unsafe.Pointer {
	var x int
	p := unsafe.Pointer(&x)
	p = other
	return p
}

func aliasLocalP() int {
	var x int
	p := unsafe.Pointer(&x)
	return *(*int)(p)
}

func constantU() uintptr {
	return uintptr(5)
}
