package panicfunction

func panic(any) {}

func direct(n int) int { // want "function direct calls itself with its own arguments on every path"
	panic("returning function")
	return direct(n)
}

func parenthesized(n int) int { // want "function parenthesized calls itself with its own arguments on every path"
	(panic)("returning function")
	return parenthesized(n)
}

func branch(n int) int { // want "function branch calls itself with its own arguments on every path"
	if n > 0 {
		panic("returning function")
	}
	return branch(n)
}
