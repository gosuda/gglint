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

func closedrecvSilentAfter() int {
	ch := make(chan int)
	sum := 0
	for {
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

func closedrecvSilentLenBuiltin() {
	ch := make(chan int)
	go func() { _ = len(ch) }()
	sum := 0
	for {
		sum += <-ch
	}
	use(sum)
}

func closedrecvSilentReturn() int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	for {
		return <-ch
	}
}

func closedrecvSilentBreak() int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		break
	}
	return sum
}

func closedrecvSilentBreakIf() int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		v := <-ch
		sum += v
		if sum > 0 {
			break
		}
	}
	return sum
}

func closedrecvSilentUncalled() int {
	ch := make(chan int)
	stop := func() { go func() { close(ch) }() }
	_ = stop
	sum := 0
	for {
		sum += <-ch
	}
	return sum
}

type closedrecvNop struct{}

func (closedrecvNop) M(ch chan int) {}

func closedrecvSilentGoto() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch); closedrecvNop{}.M(ch) }()
	sum := 0
	for {
		sum += <-ch
		goto Done
	}
Done:
	use(sum)
}

func closedrecvSilentBlock() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		{
			break
		}
	}
	use(sum)
}

func closedrecvSilentIfElse() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		if sum > 0 {
			break
		} else {
			return
		}
	}
}

func closedrecvSilentElseIf() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		if sum > 0 {
			break
		} else if sum < 0 {
			return
		} else {
			return
		}
	}
}

func closedrecvSilentLabeledBreak() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
Outer:
	for {
		sum += <-ch
		break Outer
	}
	use(sum)
}

func closedrecvReportedInnerBreak() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch
		neg := -sum
		_ = neg
	Inner:
		for i := 0; i < 3; i++ {
			break Inner
		}
	}
	use(sum)
}

func closedrecvSilentContinueOuter() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
Outer:
	for i := 0; i < 3; i++ {
		for {
			sum += <-ch
			continue Outer
		}
	}
	use(sum)
}

func closedrecvReportedContinueSelf() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
Outer:
	for { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch
		continue Outer
	}
}

func closedrecvSilentNestedFor() int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		for i := 0; i < 3; i++ {
			return sum
		}
	}
}

func closedrecvSilentNestedRange(xs []int) int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		for _, v := range xs {
			return v
		}
	}
}

func closedrecvSilentNestedSwitch(n int) int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		switch n {
		case 0:
			return 0
		default:
			n++
		}
	}
}

func closedrecvSilentNestedTypeSwitch(v any) int {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	sum := 0
	for {
		sum += <-ch
		switch v.(type) {
		case int:
			return 1
		default:
		}
	}
}

func closedrecvReportedInvoked() {
	ch := make(chan int)
	func() { go func() { ch <- 1; close(ch) }() }()
	sum := 0
	for { // want `after close, this receive from ch returns zero values at once, so the loop spins: use the two-value receive and stop when the second value is false`
		sum += <-ch
	}
}
