package errchainreset

import "fmt"

type codeErr struct{ code int }

func (e *codeErr) Error() string { return "code" }

type notError struct{}

func (notError) Error(n int) string { return "not an error" }

const constFormat = "const: %v"

func verbV(err error) error {
	return fmt.Errorf("read: %v", err) // want `fmt\.Errorf formats error operand err with %v: use %w to keep the error chain`
}

func verbT(err error) error {
	return fmt.Errorf("type: %T", err) // want `fmt\.Errorf formats error operand err with %T`
}

func rawLiteral(err error) error {
	return fmt.Errorf(`read: %+v`, err) // want `fmt\.Errorf formats error operand err with %\+v`
}

func errorCall(err error) error {
	return fmt.Errorf("read: %s", err.Error()) // want `fmt\.Errorf passes err\.Error\(\): pass the error itself with %w to keep the error chain`
}

func parenErrorCall(err error) error {
	return fmt.Errorf("read: %v", (err.Error())) // want `fmt\.Errorf passes err\.Error\(\)`
}

func quotedErrorCall(err error) error {
	return fmt.Errorf("read: %q", err.Error()) // want `fmt\.Errorf passes err\.Error\(\)`
}

func concatErrorCall(err error) error {
	return fmt.Errorf("read: " + err.Error()) // want `fmt\.Errorf passes err\.Error\(\)`
}

func concreteErrorCall(ce *codeErr) error {
	return fmt.Errorf("code: %s", ce.Error()) // want `fmt\.Errorf passes ce\.Error\(\)`
}

func concreteOperand(ce *codeErr) error {
	return fmt.Errorf("code: %v", ce) // want `fmt\.Errorf formats error operand ce with %v`
}

func twoOperands(a, b error) error {
	return fmt.Errorf("%v and %s", a, b) // want `fmt\.Errorf formats error operand a with %v`
}

func wrappedAndPlain(err, other error) error {
	return fmt.Errorf("%w: %v", other, err) // want `fmt\.Errorf formats error operand err with %v`
}

func indexedOperand(err error, n int) error {
	return fmt.Errorf("%[2]v %[1]d", n, err) // want `fmt\.Errorf formats error operand err with %\[2\]v`
}

func indexedStarWidth(err error) error {
	return fmt.Errorf("%[1]*d", 3, err) // want `fmt\.Errorf formats error operand err with %\[1\]\*d`
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

func dynamicFormatErrorCall(format string, err error) error {
	return fmt.Errorf(format, err.Error()) // want `fmt\.Errorf passes err\.Error\(\)`
}

func partlyFixable(err error) error {
	return fmt.Errorf("%v: %q", err, err.Error()) // want `fmt\.Errorf formats error operand err with %v`
}

func inFuncLit() {
	wrap := func(e error) error { return fmt.Errorf("wrap: %v", e) } // want `fmt\.Errorf formats error operand e with %v`
	_ = wrap
}

func wrapW(err error) error {
	return fmt.Errorf("read: %w", err)
}

func wrapTwice(a, b error) error {
	return fmt.Errorf("%w and %w", a, b)
}

func wrapIndexed(err error, n int) error {
	return fmt.Errorf("%[2]w %[1]d", n, err)
}

func wrapWithWidth(err error) error {
	return fmt.Errorf("%*d %w", 3, 4, err)
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

func trailingPercent(err error) error {
	return fmt.Errorf("bad %", err)
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

func noArguments() error {
	return fmt.Errorf("plain")
}
