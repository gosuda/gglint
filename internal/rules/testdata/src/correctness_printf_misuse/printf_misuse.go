// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/printf_misuse_go.go.

// errorcheck

// correctness::printf_misuse on a Go package: validates format string verbs
// against argument count and operand types for formatted I/O calls.
// In Go packages, forbid lowers to deny, so //logos:allow can silence it.

package p

import (
	"fmt"
	"os"
)

func badArgCount() {
	fmt.Printf("user %s count %d\n", "alice") // want `fmt\.Printf format %d reads arg #2, but call has 1 arg`
	fmt.Sprintf("user %s count %d\n")         // want `fmt\.Sprintf format %s reads arg #1, but call has 0 args`
	fmt.Fprintf(os.Stdout, "%s %s\n", "one")  // want `fmt\.Fprintf format %s reads arg #2, but call has 1 arg`
	fmt.Printf("count %d\n", 1, 2)            // want `fmt\.Printf call needs 1 arg but has 2 args`
}

func badType() {
	fmt.Printf("%d\n", "not an int") // want `fmt\.Printf format %d has arg "not an int" of wrong type string`
	fmt.Printf("%s\n", 42)           // want `fmt\.Printf format %s has arg 42 of wrong type int`
	fmt.Printf("%t\n", 123)          // want `fmt\.Printf format %t has arg 123 of wrong type int`
}

func badIndex() {
	fmt.Printf("%[0]d\n", 1) // want `fmt\.Printf format has invalid argument index \[0\]`
}

func cleanForms(dynamic string) {
	fmt.Printf("user %s count %d\n", "alice", 42)
	fmt.Sprintf("val: %v, str: %s, int: %d\n", true, "ok", 10)
	fmt.Fprintf(os.Stdout, "name: %s\n", "bob")
	fmt.Printf("%v %+v %#v %T\n", "s", 1, true, struct{}{})
	fmt.Printf(dynamic, 1, 2, 3)
}
