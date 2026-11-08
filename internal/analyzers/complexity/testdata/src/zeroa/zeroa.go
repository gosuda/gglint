package zeroa // want `^file is too long: line count 15 exceeds 1$`

type Two interface {
	A()
	B()
}

func All(a, b bool) int { // want `^function All is too complex: parameter count 2 exceeds 1, cognitive score 3 exceeds 1, bool parameter count 2 exceeds 1$`
	if a {
		if b {
			return 1
		}
	}
	return 0
}
