package unsafeuse

import "unsafe"

func words(p unsafe.Pointer) uintptr {
	return uintptr(p) / wordSize
}
