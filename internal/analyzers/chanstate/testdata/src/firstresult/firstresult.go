package firstresult

func useFirst(v int) { _ = v }

func workFirst() {}

func firstresultReported() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and 1 values are received with capacity 0`
	}
	return <-ch
}

func firstresultSilent() int {
	ch := make(chan int, 3)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	return <-ch
}

func firstresultSilentCall() {
	ch := make(chan int)
	go workFirst()
	_ = ch
}

func firstresultSilentTwo() {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i; ch <- 2 }()
	}
}

func firstresultSilentRange() {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	for v := range ch {
		useFirst(v)
	}
}

func firstresultSilentParam(ch chan int) int {
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	return <-ch
}
