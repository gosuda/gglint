package standard

import "errors"

const pending = "not implemented"

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

func todoInSentence() {
	panic("parser TODO: tabs") // want `placeholder panic: implement this path`
}

func shouting() {
	panic("UNIMPLEMENTED on this platform") // want `placeholder panic: implement this path`
}

func rawString() {
	panic(`not implemented`) // want `placeholder panic: implement this path`
}

func parenthesizedArg() {
	panic(("not implemented")) // want `placeholder panic: implement this path`
}

func constantName() {
	panic(pending) // want `placeholder panic: implement this path`
}

func constantConcat() {
	panic("feature " + "unimplemented") // want `placeholder panic: implement this path`
}

func todoLower() {
	panic("todo")
}

func todoMixed() {
	panic(`Todo: later`)
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

func parenthesizedPanic() {
	(panic)("unimplemented")
}

func otherBuiltin() {
	println("unimplemented")
}
