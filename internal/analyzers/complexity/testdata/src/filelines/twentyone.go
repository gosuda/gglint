package filelines // want `^file is too long: line count 21 exceeds 20$`

func twentyOne() int {
	a := 0
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
	a++
	a++
	a++
	a++
	a++
	return a
}
