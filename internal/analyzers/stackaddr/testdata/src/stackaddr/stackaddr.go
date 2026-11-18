package stackaddr

import (
	"reflect"
	"unsafe"
)

type address uintptr
type rawPointer = unsafe.Pointer

func allocatedPointer() unsafe.Pointer {
	return unsafe.Pointer(new(int))
}

func localPointer() unsafe.Pointer {
	var x int
	return unsafe.Pointer(&x)
}

func integerAddress() uintptr {
	var x int
	u := uintptr(unsafe.Pointer(&x))
	return u
}

func immediate(p unsafe.Pointer, offset uintptr) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + offset)
}

func offsetFirst(p unsafe.Pointer, offset uintptr) unsafe.Pointer {
	return unsafe.Pointer(offset + uintptr(p))
}

func masked(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) &^ 7)
}

func savedAddress(p unsafe.Pointer) unsafe.Pointer {
	u := uintptr(p)
	return unsafe.Pointer(u) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func arbitraryAddress(u uintptr) unsafe.Pointer {
	return unsafe.Pointer(u) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func aliasAddress(u address) rawPointer {
	return rawPointer(u) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func twoPointers(p, q unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p)+uintptr(q)) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func hiddenPointer(p, q unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p)+uintptr(uint64(uintptr(q)))) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func reflection(v reflect.Value) unsafe.Pointer {
	return unsafe.Pointer(v.Pointer())
}

func reflectionMethod(v reflect.Value) unsafe.Pointer {
	return unsafe.Pointer(reflect.Value.UnsafeAddr(v))
}

func savedReflection(v reflect.Value) unsafe.Pointer {
	u := v.Pointer()
	return unsafe.Pointer(u) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func fabricatedHeader() *reflect.SliceHeader {
	return &reflect.SliceHeader{} // want `possible misuse of reflect\.SliceHeader value; keep a pointer to the original slice or string`
}

func copiedHeader(h *reflect.StringHeader) reflect.StringHeader {
	return *h // want `possible misuse of reflect\.StringHeader value; keep a pointer to the original slice or string`
}

func headerValue(h reflect.SliceHeader) unsafe.Pointer {
	return unsafe.Pointer(h.Data) // want `possible misuse of unsafe\.Pointer: uintptr does not keep its referent alive; convert directly in the same expression`
}

func headerOverlay(s []int) unsafe.Pointer {
	h := (*reflect.SliceHeader)(unsafe.Pointer(&s))
	return unsafe.Pointer(h.Data)
}
