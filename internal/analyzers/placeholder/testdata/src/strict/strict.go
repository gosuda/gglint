package strict

// TODO: write the real parser // want `placeholder TODO comment: finish the work or delete the marker`
func leadingTodo() {}

// FIXME handle the overflow // want `placeholder FIXME comment: finish the work or delete the marker`
func leadingFixme() {}

// TODO(alice): owner in parentheses // want `placeholder TODO comment`
func ownedTodo() {}

// FIXME, the cache leaks // want `placeholder FIX.E comment`
func commaFixme() {}

// TODO // want `placeholder TODO comment`
func bareTodo() {}

/* FIXME: block comment */ // want `placeholder FIX.E comment`
func blockFixme()          {}

/*TODO*/            // want `placeholder TODO comment`
func blockNoSpace() {}

func bodyComments() {
	//TODO: no space after the slashes // want `placeholder TODO comment`
	//	TODO: a tab after the slashes // want `placeholder TODO comment`
}

func trailing() {
	x := 1 // TODO: trailing marker // want `placeholder TODO comment`
	_ = x
}

func panicStub() {
	panic("TODO: later") // want `placeholder panic: implement this path`
}

func panicLower() {
	panic("todo")
}

// Parse reads input; TODO handle tabs.
func midSentence() {}

func proseTrailing() {
	x := 1 // see the TODO list
	_ = x
}

// TODOS are tracked in the issue tracker.
func plural() {}

// TODO_LIST and FIXME2 are identifiers, not markers.
func glued() {}

// todo in lower case is prose.
func lowerCase() {}

// Fixme in mixed case is prose.
func mixedCase() {}

//go:generate echo TODO
func generateDirective() {}

//nolint:printf // TODO: revisit the error policy
func nolintDirective() {}

func notAComment() string {
	return "// TODO: this is data, not a comment"
}
