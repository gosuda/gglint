package testskip

func latest(a int) int { // want `^function latest is too complex: line count 13 exceeds 12$`
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
