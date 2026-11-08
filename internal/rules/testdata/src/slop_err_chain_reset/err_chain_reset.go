// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/err_chain_reset_go.go.

// errorcheck

package p

import (
	"errors"
	"fmt"
	"os"
)

// M9-S2 slop::err_chain_reset (SPEC Sec 1.2), Go twin: logos lint applies
// the rule to Go packages. fmt.Errorf that formats an error operand with a
// verb other than %w, or that passes err.Error(), cuts the chain errors.Is
// and errors.As follow. The rule is deny, so the findings below fail the
// lint run and //logos:allow silences one.

var errBase = errors.New("base")

type codeErr struct{ code int }

func (e *codeErr) Error() string { return "code" }

type notError struct{}

func (notError) Error(n int) string { return "not an error" }

const constFormat = "const: %v"

func verbV(err error) error {
	return fmt.Errorf("read: %v", err) // want `fmt\.Errorf formats error operand err with %v`
}

func verbS(err error) error {
	return fmt.Errorf("read: %s", err) // want `fmt\.Errorf formats error operand err with %s`
}

func verbQ(err error) error {
	return fmt.Errorf("read: %q", err) // want `fmt\.Errorf formats error operand err with %q`
}

func verbPlusV(err error) error {
	return fmt.Errorf("read: %+v", err) // want `fmt\.Errorf formats error operand err with %\+v`
}

func verbT(err error) error {
	return fmt.Errorf("type: %T", err) // want `fmt\.Errorf formats error operand err with %T`
}

func errorCallS(err error) error {
	return fmt.Errorf("read: %s", err.Error()) // want `fmt\.Errorf passes err\.Error\(\)`
}

func errorCallV(err error) error {
	return fmt.Errorf("read: %v", err.Error()) // want `fmt\.Errorf passes err\.Error\(\)`
}

func errorCallConcat(err error) error {
	return fmt.Errorf("read: " + err.Error()) // want `fmt\.Errorf passes err\.Error\(\)`
}

func concreteOperand(ce *codeErr) error {
	return fmt.Errorf("code: %v", ce) // want `fmt\.Errorf formats error operand ce with %v`
}

func concreteErrorCall(ce *codeErr) error {
	return fmt.Errorf("code: %s", ce.Error()) // want `fmt\.Errorf passes ce\.Error\(\)`
}

func secondOperand(name string, err error) error {
	return fmt.Errorf("%s: %v", name, err) // want `fmt\.Errorf formats error operand err with %v`
}

func thirdOperand(a, b int, err error) error {
	return fmt.Errorf("%d %d %v", a, b, err) // want `fmt\.Errorf formats error operand err with %v`
}

func indexedOperand(err error, n int) error {
	return fmt.Errorf("%[2]v %[1]d", n, err) // want `fmt\.Errorf formats error operand err with %\[2\]v`
}

func starWidth(err error) error {
	return fmt.Errorf("%*d %v", 3, 4, err) // want `fmt\.Errorf formats error operand err with %v`
}

func percentLiteral(err error) error {
	return fmt.Errorf("100%% %v", err) // want `fmt\.Errorf formats error operand err with %v`
}

func constantFormat(err error) error {
	return fmt.Errorf(constFormat, err) // want `fmt\.Errorf formats error operand err with %v`
}

func wrappedAndPlain(err, other error) error {
	return fmt.Errorf("%w: %v", other, err) // want `fmt\.Errorf formats error operand err with %v`
}

func inClosure(err error) func() error {
	return func() error {
		return fmt.Errorf("x: %v", err) // want `fmt\.Errorf formats error operand err with %v`
	}
}

func inFuncLit() {
	wrap := func(e error) error { return fmt.Errorf("wrap: %v", e) } // want `fmt\.Errorf formats error operand e with %v`
	_ = wrap
}

func afterRemove(path string, inner error) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return fmt.Errorf("after remove: %v", inner) // want `fmt\.Errorf formats error operand inner with %v`
}

func wrapW(err error) error {
	return fmt.Errorf("read: %w", err)
}

func wrapTwice(a, b error) error {
	return fmt.Errorf("%w and %w", a, b)
}

func wrapWithWidth(err error) error {
	return fmt.Errorf("%*d %w", 3, 4, err)
}

func wrapIndexed(err error, n int) error {
	return fmt.Errorf("%[2]w %[1]d", n, err)
}

func wrapFuncLit() {
	wrap := func(e error) error { return fmt.Errorf("wrap: %w", e) }
	_ = wrap
}

func nonErrorOperands(n int, s string, c *codeErr) error {
	return fmt.Errorf("n=%v s=%s c=%d", n, s, c.code)
}

func anyOperand(v any) error {
	return fmt.Errorf("v=%v", v)
}

func nilOperand() error {
	return fmt.Errorf("x: %v", nil)
}

func sprintfNotErrorf(err error) string {
	return fmt.Sprintf("read: %v", err)
}

func dynamicSpread(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func dynamicFormat(format string, err error) error {
	return fmt.Errorf(format, err)
}

func otherErrorMethod(t notError) error {
	return fmt.Errorf("t: %s", t.Error(1))
}

func errorOutsideErrorf(err error) string {
	return err.Error()
}

func plainString(s string) error {
	return fmt.Errorf("plain %s", s)
}
