package params

func scale(value, factor int) int { // want `scale - factor is unused`
	return value * 2
}

func double(value int) int {
	return value * 2
}

func triple(_ int, value int) int {
	return value * 3
}

func Exported(value, factor int) int {
	return value * 4
}

func Use(a, b int) int {
	return scale(a, b) + scale(b, a) + double(a) + double(b) + triple(a, b) + triple(b, a)
}
