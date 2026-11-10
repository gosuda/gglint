package closedrecv

func use(any) {}

func worker(ch chan int) {
	ch <- 1
	close(ch)
}

func closedrecvReported() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	sum := 0
	for { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch
	}
}

func closedrecvSilent() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	sum := 0
	for {
		v, ok := <-ch
		if !ok {
			break
		}
		sum += v
	}
	use(sum)
}

func closedrecvSilentVarSpec() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	sum := 0
	for {
		var v, ok = <-ch
		if !ok {
			break
		}
		sum += v
	}
	use(sum)
}

func closedrecvSilentParam(ch chan int) {
	go func() {
		ch <- 1
		close(ch)
	}()
	sum := 0
	for {
		sum += <-ch
	}
	use(sum)
}

func closedrecvSilentNoClose() {
	ch := make(chan int)
	sum := 0
	for {
		sum += <-ch
	}
	use(sum)
}

func closedrecvSilentPost() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	sum := 0
	for i := 0; i < 3; i++ {
		sum += <-ch
	}
	use(sum)
}

func closedrecvSilentIndex() {
	chs := []chan int{make(chan int)}
	sum := 0
	for {
		sum += <-chs[0]
	}
	use(sum)
}

func closedrecvSilentSelect() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	for {
		select {
		case v := <-ch:
			use(v)
		}
	}
}

func closedrecvSilentSend() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	for {
		ch <- 1
	}
}

func closedrecvSilentCall() {
	ch := make(chan int)
	go func() {
		ch <- 1
		close(ch)
	}()
	for {
		use(<-ch)
	}
}

func closedrecvSilentNamedGo() {
	ch := make(chan int)
	go worker(ch)
	sum := 0
	for {
		sum += <-ch
	}
	use(sum)
}

func closedrecvSilentDelete() {
	ch := make(chan int)
	m := map[int]int{}
	go func() {
		delete(m, 1)
	}()
	sum := 0
	for {
		sum += <-ch
	}
	use(sum)
}

func closedrecvReportedInitPost() int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for i := 0; ; i++ { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch + i
	}
}

func closedrecvReportedAfter() int {
	ch := make(chan int)
	sum := 0
	for { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch
		if sum > 9 {
			break
		}
	}
	go func() { ch <- 1; close(ch) }()
	return sum
}

func closedrecvReportedLen(buf []int) int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch + len(buf)
	}
}
