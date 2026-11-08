// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/discarded_error_go.go.

// errorcheck

package p

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// M9-S2 slop::discarded_error (SPEC Sec 1.2, #164), Go twin: logos lint
// applies the rule to Go packages. An expression statement or `_ =` that
// drops an `error` result is reported, except callees with an
// AlwaysNilError fact, fmt.Print*, fmt.Fprint* to os.Stdout or os.Stderr,
// and the Write* methods of *bytes.Buffer and *strings.Builder.
// fmt.Fprint* to any other destination is reported. The rule is deny, so
// the findings below fail the lint run.

func f() error        { return nil }
func g() (int, error) { return 0, nil }

func dropped() {
	f()         // want `error result of f\(\) is dropped; handle or propagate it`
	g()         // want `error result of g\(\) is dropped; handle or propagate it`
	_ = f()     // want `error result of f\(\) is dropped; handle or propagate it`
	_, _ = g()  // want `error result of g\(\) is dropped; handle or propagate it`
	n, _ := g() // want `error result of g\(\) is dropped; handle or propagate it`
	_ = n
	h := func() error { return f() }
	h()       // want `error result of h\(\) is dropped; handle or propagate it`
	_ = (f()) // want `error result of f\(\) is dropped; handle or propagate it`
}

func printed() {
	fmt.Print("x")
	fmt.Printf("%d", 1)
	fmt.Println("x")
	fmt.Fprintf(os.Stderr, "x")
	fmt.Fprintln(os.Stdout, "x")
}

func written() {
	var b bytes.Buffer
	b.WriteString("x")
	b.WriteByte('x')
	var sb strings.Builder
	sb.WriteString("x")
	sb.WriteByte('x')
}

func printedToBuffer() {
	var b bytes.Buffer
	fmt.Fprintf(&b, "x") // want `error result of fmt\.Fprintf\(&b, "x"\) is dropped; handle or propagate it`
	var sb strings.Builder
	fmt.Fprintf(&sb, "x") // want `error result of fmt\.Fprintf\(&sb, "x"\) is dropped; handle or propagate it`
}

func handled() {
	err := f()
	_ = err
	defer f()
	m, err := g()
	_, _ = m, err
}

func inErrorFunc() error {
	f() // want `error result of f\(\) is dropped; handle or propagate it`
	return nil
}
