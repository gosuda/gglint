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

