// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/zeroalloc_go.go.

// errorcheck

// //logos:zeroalloc on a Go package (R-U1): logos lint runs the check inside
// escape analysis of Go code, so the Go twin of test/logos/codegen/zeroalloc.lgo
// reports the same allocation sites. Only logos lint reads the directive in a
// Go package; logos build and go build ignore it, which logos_zeroalloc.txt
// proves.

package p

type point struct{ x, y int }

// +checkescape:heap
func total(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

// +checkescape:heap
func pack(v int) []int {
	return []int{v} // want "heap: explicit allocation"
}

// +checkescape:heap
func (p *point) clone() *point {
	return &point{p.x, p.y} // want "heap: explicit allocation"
}

// +checkescape:heap
func constantPanic(i int, xs []int) int {
	if i < 0 {
		panic("negative index")
	}
	return xs[i]
}

// unmarked allocates freely: only marked functions are checked.
func unmarked(v int) []int {
	return []int{v}
}
