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

func doublecloseSilentAddress() {
	ch := make(chan int)
	close(ch)
	p := &ch
	*p = make(chan int)
	close(ch)
	_ = p
}

func doublecloseSilentRange(chans []chan int) {
	ch := make(chan int)
	close(ch)
	for _, ch = range chans {
	}
	close(ch)
}

func doublecloseSilentGo() {
	ch := make(chan int, 1)
	go close(ch)
	close(ch)
}
