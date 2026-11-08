// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/typed_nil_return_go.go.

// errorcheck

// correctness::typed_nil_return on a Go package (SPEC Sec 1.2, T-102, R-U1):
// only the (*T)(nil) form is active, because options and platform pointers do
// not exist in Go. `logos lint` runs the rule at deny here, so a
// //logos:allow directive silences it. A plain nil pointer variable returned
// as an interface is not reported: that needs the nilness facts of loGo/s.

package p

import (
	"io"
	"os"
)

var touched bool

type Err struct{ msg string }

func (e *Err) Error() string { return e.msg }

func explicitError() error {
	return (*Err)(nil) // want `returns typed nil \*Err as interface error:`
}

func explicitParen() error {
	return ((*Err)(nil)) // want `returns typed nil \*Err as interface error:`
}

func explicitAny() any {
	return (*Err)(nil) // want `returns typed nil \*Err as interface any:`
}

func explicitCloser() io.Closer {
	return (*os.File)(nil) // want `returns typed nil \*os\.File as interface io\.Closer:`
}

func explicitSecond() (int, error) {
	return 1, (*Err)(nil) // want `returns typed nil \*Err as interface error:`
}

func explicitClosure() func() error {
	return func() error {
		return (*Err)(nil) // want `returns typed nil \*Err as interface error:`
	}
}

func explicitUnderGuard(e *Err) error {
	if e == nil {
		return (*Err)(nil) // want `returns typed nil \*Err as interface error:`
	}
	return nil
}

func nilIsFine() error {
	return nil
}

func freshPointerIsFine() error {
	return &Err{msg: "x"}
}

func notAnInterface() *Err {
	return (*Err)(nil)
}

func notTheInterfaceSlot() (*Err, error) {
	return (*Err)(nil), nil
}

func otherNilConversionIsFine() any {
	return []int(nil)
}

func variableIsInactiveInGo(p *Err) error {
	return p
}

func nilVariableIsInactiveInGo() error {
	var p *Err
	return p
}

func platformPointerIsInactiveInGo() io.Closer {
	return os.Stdout
}

func namedResultIsInactiveInGo() (err error) {
	err = (*Err)(nil)
	return
}

func allowOnlyCoversItsStatement() error {
	touched = true
	return (*Err)(nil) // want `returns typed nil \*Err as interface error:`
}
