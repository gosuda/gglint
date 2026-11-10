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
