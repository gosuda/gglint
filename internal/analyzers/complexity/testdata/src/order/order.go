package order

func Small(a bool) int {
	if a {
		return 1
	}
	return 0
}

func All(a, b bool) int { // want `^function All is too complex: nesting depth 2 exceeds 1, parameter count 2 exceeds 1, line count 8 exceeds 6, cognitive score 3 exceeds 2, cyclomatic complexity 3 exceeds 2, bool parameter count 2 exceeds 1$`
	if a {
		if b {
			return 1
		}
	}
	return 0
}
