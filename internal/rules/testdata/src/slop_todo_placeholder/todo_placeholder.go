// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/todo_placeholder_go.go.

// errorcheck

package p

import "errors"

// M9-S2 slop::todo_placeholder (SPEC Sec 1.2), Go twin: logos lint applies
// the rule to Go packages. A comment whose text starts with the marker
// word TODO or FIXME, or a panic whose string literal says unimplemented,
// not implemented, or the marker, flags unfinished work in a non-test
// file. The rule is deny, so each finding fails the lint run and
// //logos:allow silences one.

// TODO: write the real parser // want "placeholder TODO comment"
func leadingTodo() {}

// FIXME: handle the overflow // want "placeholder FIXME comment"
func leadingFixme() {}

//TODO(alice): no space after the slashes // want "placeholder TODO comment"

func noSpace() {}

//	TODO: a tab after the slashes // want "placeholder TODO comment"

func tabbed() {}

// TODO // want "placeholder TODO comment"
func bareTodo() {}

/* TODO: block comment */ // want "placeholder TODO comment"
func blockTodo()          {}

func trailing() {
	x := 1 // TODO: trailing comment // want "placeholder TODO comment"
	_ = x
}

func inBody() {
	// FIXME: inside a function body // want "placeholder FIXME comment"
}

type fields struct {
	a int // TODO: remove a // want "placeholder TODO comment"
}

func panicUnimplemented() {
	panic("unimplemented") // want "placeholder panic: implement this path"
}

func panicNotImplemented() {
	panic("not implemented") // want "placeholder panic: implement this path"
}

func panicTodo() {
	panic("TODO") // want "placeholder panic: implement this path"
}

func panicTodoText() {
	panic("TODO: later") // want "placeholder panic: implement this path"
}

func panicCapitalized() {
	panic("Not Implemented yet") // want "placeholder panic: implement this path"
}

func panicInSentence() {
	panic("feature unimplemented on this platform") // want "placeholder panic: implement this path"
}

func panicRaw() {
	panic(`not implemented`) // want "placeholder panic: implement this path"
}

type T struct{}

func (T) method() {
	panic("unimplemented") // want "placeholder panic: implement this path"
}

// A TODO that does not start the comment is prose, not a marker.
func proseTodo() {}

// todo in lowercase is not a marker.
func lowerTodo() {}

// TODOS are tracked by the issue tracker, so the word does not start a marker.
func pluralTodo() {}

// TODOLIST neither, and FIXMEs neither.
func gluedTodo() {}

func notAMarker() {
	x := 1 // see the TODO list
	_ = x
}

var inString = "// TODO: this is data, not a comment"

var inRaw = `// FIXME: so is this`

func panicFine() {
	panic("unreachable")
}

func panicTodoLower() {
	panic("todo")
}

func panicErrorValue() {
	panic(errors.New("unimplemented"))
}

func panicVariable(msg string) {
	panic(msg)
}

func panicShadowed() {
	panic := func(string) {}
	panic("unimplemented")
}

func otherBuiltin() {
	println("unimplemented")
}

// TODO: a blank line ends the directive block // want "placeholder TODO comment"
func notCovered() {}
