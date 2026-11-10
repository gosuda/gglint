package nilop

func nilopReported() {
	var ch chan int
	ch <- 1 // want `ch is declared and never made, so this operation blocks forever \(close panics\): make the channel first`
}

func nilopSilent() {
	var ch chan int
	select {
	case ch <- 1:
	default:
	}
}

func nilopSilentAssigned() {
	var ch chan int
	ch = make(chan int, 1)
	ch <- 1
	_ = ch
}

func nilopSilentValued() {
	var ch = make(chan int)
	ch <- 1
	_ = ch
}

func nilopSilentMulti() {
	var a, b chan int
	_ = a
	_ = b
}

func nilopRangeReported() {
	var ch chan int
	for range ch { // want `ch is declared and never made, so this operation blocks forever \(close panics\): make the channel first`
	}
}

func nilopSilentAddress() {
	var ch chan int
	p := &ch
	*p = make(chan int, 1)
	ch <- 1
	_ = p
}

func nilopNoBody(ch chan int)

func nilopReportedVarInit() int {
	var ch chan int
	var v = <-ch // want `ch is declared and never made, so this operation blocks forever \(close panics\): make the channel first`
	return v
}

func nilopSilentDefineReassign() int {
	var ch chan int
	n, ch := 1, make(chan int, 1)
	ch <- n
	for v := range ch {
		return v
	}
	return 0
}
