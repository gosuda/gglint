package doubleclose

func doublecloseReported() {
	ch := make(chan int)
	close(ch)
	close(ch) // want `ch is already closed on every path to this close, which panics: close it once`
}

func doublecloseSilent(a bool) {
	ch := make(chan int)
	if a {
		close(ch)
	} else {
		close(ch)
	}
}
