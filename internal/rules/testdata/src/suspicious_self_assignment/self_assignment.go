// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/self_assignment_go.go.

// errorcheck

// suspicious::self_assignment on a Go package (LINT-RULES.md Sec 1.2): detects
// useless self-assignments (x = x, s.f = s.f).

package p

type S struct {
	f int
}

func badAssignments(x int, s S) {
	x = x     // want "self-assignment of x$"
	s.f = s.f // want `self-assignment of s\.f$`
	var a, b int
	a, b = a, b // want "self-assignment of a, b$"
	a, b = a, 1 // want "self-assignment of a$"
	_ = a
	_ = b
}

func cleanAssignments(x, y int, s, other S) {
	x = y
	s.f = other.f
}

func cleanMap(m map[string]int) {
	m["k"] = m["k"]
}
