// want package:"package local" package:"package local"
package local

import (
	_ "unsafe"

	_ "linkname/remote"
)

//go:linkname add linkname/remote.add
func add(a, b int) int

//go:linkname peek linkname/remote.peek
func peek(p *int) uintptr

// want +2 `symbol "scale" has signature "func \(int\) \(int\)", expected signature "func \(float64\) \(float64\)"`

//go:linkname scale linkname/remote.scale
func scale(x int) int

//go:linkname twoField
func twoField() {}

// want +2 `usage: //go:linkname localname \[linkname\]`

//go:linkname a b c
