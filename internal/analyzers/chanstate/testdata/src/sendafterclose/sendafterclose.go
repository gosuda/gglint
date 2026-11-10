package sendafterclose

func sendaftercloseReported() {
	ch := make(chan int, 1)
	close(ch)
	ch <- 1 // want `ch is closed on every path to this send, which panics: send before the close`
}

func sendaftercloseSilent() {
	ch := make(chan int, 1)
	defer close(ch)
	ch <- 1
}
