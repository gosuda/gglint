// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/arena_pointer_escape_go.go.

// errorcheck

// correctness::arena_pointer_escape on a Go package: an unsafe.Pointer built
// from a uintptr the conversion does not derive from a pointer in the same
// expression, and a copied or taken reflect header value, are reported.
// `logos lint` runs the rule at deny here, so a //logos:allow directive
// silences it. Twin of arena_pointer_escape.lgo with the same boundary cases.

package p

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
	return unsafe.Pointer(u) // want "possible misuse of unsafe.Pointer"
}

func arbitraryAddress(u uintptr) unsafe.Pointer {
	return unsafe.Pointer(u) // want "possible misuse of unsafe.Pointer"
}

func aliasAddress(u address) rawPointer {
	return rawPointer(u) // want "possible misuse of unsafe.Pointer"
}

func twoPointers(p, q unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + uintptr(q)) // want "possible misuse of unsafe.Pointer"
}

func hiddenPointer(p, q unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + uintptr(uint64(uintptr(q)))) // want "possible misuse of unsafe.Pointer"
}

func reflection(v reflect.Value) unsafe.Pointer {
	return unsafe.Pointer(v.Pointer())
}

func reflectionMethod(v reflect.Value) unsafe.Pointer {
	return unsafe.Pointer(reflect.Value.UnsafeAddr(v))
}

func savedReflection(v reflect.Value) unsafe.Pointer {
	u := v.Pointer()
	return unsafe.Pointer(u) // want "possible misuse of unsafe.Pointer"
}

func fabricatedHeader() *reflect.SliceHeader {
	return &reflect.SliceHeader{} // want "possible misuse of reflect.SliceHeader"
}

func copiedHeader(h *reflect.StringHeader) reflect.StringHeader {
	return *h // want "possible misuse of reflect.StringHeader"
}

func headerValue(h reflect.SliceHeader) unsafe.Pointer {
	return unsafe.Pointer(h.Data) // want "possible misuse of unsafe.Pointer"
}

func headerOverlay(s []int) unsafe.Pointer {
	h := (*reflect.SliceHeader)(unsafe.Pointer(&s))
	return unsafe.Pointer(h.Data)
}
