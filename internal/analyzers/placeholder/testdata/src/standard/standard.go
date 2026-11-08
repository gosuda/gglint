package standard

import "errors"

// TODO: comments are only reported in strict mode.
func unimplemented() {
	panic("unimplemented") // want `placeholder panic: implement this path`
}

func notImplemented() {
	panic("Not Implemented yet") // want `placeholder panic: implement this path`
}

func todoUpper() {
	panic("TODO") // want `placeholder panic: implement this path`
}

func todoMixed() {
	panic(`Todo: later`) // want `placeholder panic: implement this path`
}

func shouting() {
	panic("UNIMPLEMENTED on this platform") // want `placeholder panic: implement this path`
}

func parenthesized() {
	panic(("not implemented")) // want `placeholder panic: implement this path`
}

func invariant() {
	panic("unreachable: state machine left its states")
}

func errorValue() {
	panic(errors.New("unimplemented"))
}

func variable(msg string) {
	panic(msg)
}

func shadowed() {
	panic := func(string) {}
	panic("unimplemented")
}

func otherBuiltin() {
	println("unimplemented")
}

func constantName() {
	const msg = "not implemented"
	panic(msg)
}
