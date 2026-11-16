package panicvariable

var panic = func(any) {}

func direct(n int) int { // want "function direct calls itself with its own arguments on every path"
	panic("returning variable")
	return direct(n)
}

func parenthesized(n int) int { // want "function parenthesized calls itself with its own arguments on every path"
	(panic)("returning variable")
	return parenthesized(n)
}

func local(n int) int { // want "function local calls itself with its own arguments on every path"
	panic := func(any) {}
	panic("returning local")
	return local(n)
}
