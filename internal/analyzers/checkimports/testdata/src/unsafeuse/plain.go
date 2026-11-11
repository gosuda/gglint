package unsafeuse

import "unsafe"

const wordSize = unsafe.Sizeof(uintptr(0))
