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

func firstresultSilentBoolInit() int {
	ch := make(chan int)
	for grew := true; grew; {
		grew = false
		go func() { ch <- 1 }()
	}
	return <-ch
}

func firstresultSilentRangeConst() {
	ch := make(chan int, 5)
	for range 5 {
		_ = 1
	}
	_ = ch
}

func firstresultSilentRangeVar(xs []int) {
	ch := make(chan int, 1)
	for range xs {
		_ = 1
	}
	_ = ch
}

func firstresultSilentNonConst(n int) int {
	ch := make(chan int, n)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	return <-ch
}

func firstresultSilentNonZero() {
	ch := make(chan int, 1)
	for i := 1; i < 3; i++ {
		_ = i
	}
	_ = ch
}

func firstresultSilentNonLess() {
	ch := make(chan int, 1)
	for i := 0; i <= 3; i++ {
		_ = i
	}
	_ = ch
}

func firstresultSilentNoPost() {
	ch := make(chan int, 1)
	for i := 0; i < 3; {
		_ = i
		break
	}
	_ = ch
}

func firstresultSilentCond() {
	ch := make(chan int, 1)
	for true {
		_ = 1
		break
	}
	_ = ch
}

func firstresultSilentScope(c bool) {
	ch := make(chan int)
	if c {
		for i := 0; i < 3; i++ {
			go func() { ch <- i }()
		}
		<-ch
		<-ch
		<-ch
	}
}

func firstresultReportedIfRecv(c bool) int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and 0 values are received with capacity 0`
	}
	if c {
		<-ch
		<-ch
		<-ch
	}
	return 0
}

func firstresultSilentShape() int {
	ch := make(chan int)
	for i := 0; i+2 < 3; i++ {
		go func() { ch <- 1 }()
	}
	return <-ch
}

func firstresultSilentBreak() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		if i > 0 {
			break
		}
		go func() { ch <- i }()
	}
	return <-ch
}

func firstresultSilentInLoop() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
		<-ch
	}
	return 0
}

func firstresultSilentSpawnRange() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	go func() {
		for range ch {
		}
	}()
	return 0
}

func drainFirst(ch chan int, n int) {
	for range n {
		<-ch
	}
}

func firstresultSilentCallDrain() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	drainFirst(ch, 3)
	return 0
}

func firstresultSilentAddrTake() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		p := &i
		*p = 2
		go func() { ch <- i }()
	}
	return <-ch
}

func firstresultSilentClosureAssign() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		bump := func() { i = 2 }
		bump()
		go func() { ch <- i }()
	}
	return <-ch
}

func firstresultSilentDrainLoop() {
	done := make(chan bool)
	for range 10 {
		go func() { done <- true }()
	}
	for range 10 {
		<-done
	}
}

func firstresultSilentDrainFor() {
	done := make(chan bool)
	const P = 10
	for i := 0; i < P; i++ {
		go func() { done <- true }()
	}
	for i := 0; i < P; i++ {
		<-done
	}
}

func firstresultSilentDrainParam(xs []int) {
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() { done <- true }()
	}
	for range xs {
		<-done
	}
}

func firstresultSilentNestedSend() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() {
			if i == 0 {
				ch <- i
			}
		}()
	}
	return <-ch
}

func firstresultSilentLabeledLoop() int {
	ch := make(chan int)
	if false {
		goto LFirst
	}
LFirst:
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	for j := 0; j < 3; j++ {
		<-ch
	}
	return 0
}

func firstresultSilentIfInit() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	<-ch
	<-ch
	if v := <-ch; v >= 0 {
		_ = v
	}
	return 0
}

func firstresultReportedLabeled() int {
	ch := make(chan int)
	if false {
		goto LFan
	}
LFan:
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and 1 values are received with capacity 0`
	}
	return <-ch
}

func firstresultReportedShortCircuit(ok bool) int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and 2 values are received with capacity 0`
	}
	<-ch
	<-ch
	if ok && <-ch == 0 {
		return 1
	}
	return 0
}

func firstresultSilentIfElseBalanced(c bool) int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	if c {
		<-ch
		<-ch
		<-ch
	} else {
		<-ch
		<-ch
		<-ch
	}
	return 0
}
func firstresultReportedIfElseUnbalanced(c bool) int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and 2 values are received with capacity 0`
	}
	if c {
		<-ch
		<-ch
		<-ch
	} else {
		<-ch
		<-ch
	}
	return 0
}


func firstresultSilentMakeInLoop() {
	for i := 0; i < 3; i++ {
		out := make(chan int)
		go func() { out <- i }()
		<-out
	}
}

func firstresultSilentSelectRecvInLoop() {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
		select {
		case <-ch:
		}
	}
}
