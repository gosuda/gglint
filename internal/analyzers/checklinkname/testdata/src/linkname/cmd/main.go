// want package:"package main" package:"package main"
package main

import (
	_ "unsafe"

	_ "linkname/remote"
)

//go:linkname add linkname/remote.add
func add(a, b int) int

// want +2 `remote symbol "missing" not defined in package "linkname/remote"`

//go:linkname missing linkname/remote.missing
func missing()

//go:linkname nanotime runtime.nanotime
func nanotime() int64

func main() {
	missing()
	_ = add(1, 2) + int(nanotime())
}
