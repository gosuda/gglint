package lines

func one() int { return 1 }

func ten(a int) int {
	a++
	a++
	a++
	a++
	a++
	a++
	a++
	return a
}

func eleven(a int) int { // want `^function eleven is too complex: line count 11 exceeds 10$`
	a++
	a++
	a++
	a++
	a++
	a++
	a++
	a++
	return a
}

func blanksAndComments(a int) int { // want `^function blanksAndComments is too complex: line count 11 exceeds 10$`
	// one

	a++

	// two

	a++

	return a
}

// documented has a doc comment
// that spans three lines,
// none of which count.
func documented(a int) int {
	a++
	a++
	a++
	a++
	a++
	a++
	a++
	return a
}

func withLiteral() func() int { // want `^function withLiteral is too complex: line count 11 exceeds 10$`
	return func() int {
		a := 0
		a++
		a++
		a++
		a++
		a++
		return a
	}
}

var long = func() int { // want `^function literal is too complex: line count 11 exceeds 10$`
	a := 0
	a++
	a++
	a++
	a++
	a++
	a++
	a++
	return a
}

func wrappedSignature( // want `^function wrappedSignature is too complex: line count 11 exceeds 10$`
	a int,
	b int,
) int {
	a++
	a++
	a++
	a++
	a++
	return a + b
}
