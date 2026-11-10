package useafterfree

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

func useafterfreeReportedCgo() {
	p := C.malloc(16)
	C.free(unsafe.Pointer(p))
	*(*byte)(p) = 1 // want "is used after it was freed"
}

type freeNode struct {
	next  *freeNode
	items []byte
}

func useafterfreeReportedNilFieldAfterFree() bool {
	p := C.malloc(16)
	C.free(unsafe.Pointer(p))
	return (*freeNode)(p).next == nil // want "is used after it was freed"
}

func useafterfreeReportedLenFieldAfterFree() int {
	p := C.malloc(16)
	C.free(unsafe.Pointer(p))
	return len((*freeNode)(p).items) // want "is used after it was freed"
}
func useafterfreeSilentCgoFreeLast() byte {
	p := C.malloc(16)
	*(*byte)(p) = 1
	v := *(*byte)(p)
	C.free(unsafe.Pointer(p))
	return v
}

func useafterfreeSilentUintptrFree() {
	var u uintptr = 123
	C.free(unsafe.Pointer(u))
	u++
	_ = u
}

