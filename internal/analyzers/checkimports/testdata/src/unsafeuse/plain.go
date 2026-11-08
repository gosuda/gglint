package unsafeuse

import "unsafe" // want `package unsafe imported by plain\.go; must end with _unsafe\.go`

const wordSize = unsafe.Sizeof(uintptr(0))
