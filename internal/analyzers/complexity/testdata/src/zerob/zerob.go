package zerob

type Two interface { // want `^interface Two is too large: method count 2 exceeds 1$`
	A()
	B()
}

func All(a, b bool) int { // want `^function All is too complex: nesting depth 2 exceeds 1, line count 8 exceeds 1, cyclomatic complexity 3 exceeds 1$`
	if a {
		if b {
			return 1
		}
	}
	return 0
}
