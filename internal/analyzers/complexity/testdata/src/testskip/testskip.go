package testskip // want `^file is too long: line count 26 exceeds 25$`

func thirteen(a int) int { // want `^function thirteen is too complex: line count 13 exceeds 12$`
	a++
	a++
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

func deep(n int) int { // want `^function deep is too complex: nesting depth 3 exceeds 2$`
	for range n {
		for range n {
			if n > 1 {
				return n
			}
		}
	}
	return 0
}
