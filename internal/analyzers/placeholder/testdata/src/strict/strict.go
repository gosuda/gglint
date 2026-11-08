package strict

// TODO: write the real parser // want `placeholder TODO comment: finish the work or delete the marker`
func leadingTodo() {}

// FIXME handle the overflow // want `placeholder FIXME comment: finish the work or delete the marker`
func leadingFixme() {}

// TODO(alice): owner in parentheses // want `placeholder TODO comment`
func ownedTodo() {}

// Parse reads input; TODO handle tabs. // want `placeholder TODO comment`
func midSentence() {}

/* FIXME: block comment */ // want `placeholder FIX.E comment`
func blockFixme() {}

func trailing() {
	x := 1 // TODO: trailing marker // want `placeholder TODO comment`
	_ = x
}

func panicStub() {
	panic("todo") // want `placeholder panic: implement this path`
}

// TODOS are tracked in the issue tracker.
func plural() {}

// autoTODO and TODO_LIST are identifiers, not markers.
func glued() {}

// The autodoc generator writes FIXMEs elsewhere.
func prose() {}

// todo in lower case is prose.
func lowerCase() {}

//go:generate echo TODO
func generateDirective() {}

//nolint:errcheck // TODO: revisit the error policy
func nolintDirective() {}

//lint:ignore U1000 TODO keep for the next release
func lintDirective() {}

// +build ignore TODO
func buildDirective() {}

func notAComment() string {
	return "// TODO: this is data, not a comment"
}
