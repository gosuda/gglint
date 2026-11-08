// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/defer_in_loop_go.go.

// errorcheck -0

// suspicious::defer_in_loop on a Go package (LINT-RULES.md Sec 1.2): detects
// defer inside loops delaying resource release until function exit.

package p

func cleanup() {}

func badLoop(files []string) {
	for range files {
		defer cleanup() // want "deferInLoop: Possible resource leak, 'defer' is called in the 'for' loop"
	}
	for i := range len(files) {
		if i%2 == 0 {
			defer cleanup() // want "deferInLoop: Possible resource leak, 'defer' is called in the 'for' loop"
		}
	}
}

func cleanLoop(files []string) {
	defer cleanup()
	for range files {
		func() {
			defer cleanup()
		}()
	}
}
