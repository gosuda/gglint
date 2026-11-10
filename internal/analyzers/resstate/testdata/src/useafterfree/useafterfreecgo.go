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

