// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/infinite_recursion_go.go.

// errorcheck

// correctness::infinite_recursion on a Go package (SPEC Sec 1.2, #157,
// R-U1): a function whose every path from entry calls itself with identical
// arguments before any return. `logos lint` runs the rule at deny here, so
// a //logos:allow directive silences it. Twin of
// infinite_recursion.lgo with the same boundary cases.

package p

func selfDirect(n int) int { return selfDirect(n) } // want "function selfDirect calls itself with its own arguments on every path"

func selfAfterCall(n int) int { // want "function selfAfterCall calls itself with its own arguments on every path"
	if n > 0 {
		selfAfterCall(n)
		return n
	}
	return selfAfterCall(n)
}

func selfFallthrough(n int) int { // want "function selfFallthrough calls itself with its own arguments on every path"
	if n > 0 {
		selfFallthrough(n)
	}
	return selfFallthrough(n)
}

func selfSwitch(n int) int { // want "function selfSwitch calls itself with its own arguments on every path"
	switch n {
	case 0:
		return selfSwitch(n)
	default:
		return selfSwitch(n)
	}
}

func selfSwitchFallthrough(n int) int { // want "function selfSwitchFallthrough calls itself with its own arguments on every path"
	switch n {
	case 0:
		return selfSwitchFallthrough(n)
	case 1:
		return selfSwitchFallthrough(n)
	}
	return selfSwitchFallthrough(n)
}

func selfNested(n int) int { // want "function selfNested calls itself with its own arguments on every path"
	if n > 0 {
		if n > 1 {
			return selfNested(n)
		}
	}
	return selfNested(n)
}

func nestedNoCall(n int) int {
	if n > 0 {
		if n > 1 {
			return n
		}
	}
	return nestedNoCall(n)
}

func guarded(n int) int {
	if n > 0 {
		return n
	}
	return guarded(n)
}

func partialSwitch(n int) int {
	switch n {
	case 0:
		return 0
	default:
		return partialSwitch(n)
	}
}

func switchNoDefault(n int) int {
	switch n {
	case 0:
		return switchNoDefault(n)
	}
	return 0
}

func differentArgs(n int) int { return differentArgs(n - 1) }

func reassigned(n int) int {
	n--
	return reassigned(n)
}

// Cases pinned by the M9-S2 callee-rule slice.

func loop(n int) int { return loop(n) } // want "function loop calls itself with its own arguments on every path"

func tick() { tick() } // want "function tick calls itself with its own arguments on every path"

type counter struct{ n int }

func (c counter) next(n int) int { return c.next(n) } // want "function next calls itself with its own arguments on every path"

func guardedDecrement(n int) int {
	if n <= 0 {
		return 0
	}
	return guardedDecrement(n - 1)
}

func countdown(n int) int {
	if n == 0 {
		return 0
	}
	return countdown(n - 1)
}

func bump(n int) int {
	n++
	return bump(n)
}

func ping(n int) int { return pong(n) }

func pong(n int) int { return ping(n) }

func read(n int) int {
	println(n)
	return n
}
