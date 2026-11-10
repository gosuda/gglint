package firstresult

import (
	"log"
	"os"
	"sync"
)

func useFirst(v int) { _ = v }

func workFirst() {}

func firstresultReported() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and at most 1 values are received with capacity 0`
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
	for range 3 {
		go workFirst()
	}
}

func firstresultSilentTwo() {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i; ch <- 2 }()
	}
	for range 6 {
		<-ch
	}
}

func firstresultSilentRange() {
	ch := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ch <- i }()
	}
	go func() { wg.Wait(); close(ch) }()
	for v := range ch {
		useFirst(v)
	}
}

func firstresultSilentParam(ch chan int) int {
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	for range 3 {
		<-ch
	}
	return 0
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
	ch := make(chan int)
	for range 5 {
		go func() { ch <- 1 }()
	}
	for range 5 {
		<-ch
	}
}

func firstresultSilentRangeVar(xs []int) {
	ch := make(chan int)
	for range xs {
		go func() { ch <- 1 }()
	}
	for range xs {
		<-ch
	}
}

func firstresultSilentNonConst(n int) int {
	ch := make(chan int, n)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	for range 3 {
		<-ch
	}
	return 0
}

func firstresultSilentNonZero() {
	ch := make(chan int, 1)
	for i := 1; i < 3; i++ {
		go func() { ch <- i }()
	}
	<-ch
	<-ch
}

func firstresultSilentNonLess() {
	ch := make(chan int)
	for i := 0; i <= 3; i++ {
		go func() { ch <- i }()
	}
	for range 4 {
		<-ch
	}
}

func firstresultSilentNoPost() {
	ch := make(chan int, 1)
	for i := 0; i < 3; {
		go func(v int) { ch <- v }(i)
		i++
	}
	for range 3 {
		<-ch
	}
}

func firstresultSilentCond() {
	ch := make(chan int)
	for true {
		go func() { ch <- 1 }()
		break
	}
	<-ch
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
	<-ch
	<-ch
	return <-ch
}

func firstresultSilentClosureAssign() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		bump := func() { i = 2 }
		bump()
		go func() { ch <- i }()
	}
	<-ch
	<-ch
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
	for i := 0; i < len(xs); i++ {
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
		go func() { ch <- i }() // want `3 goroutines send on ch and at most 1 values are received with capacity 0`
	}
	return <-ch
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

func firstresultSilentIfElseNoReturn() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	if err := workFirstErr(); err == nil {
		a := <-ch
		b := <-ch
		c := <-ch
		_ = a + b + c
	} else {
		panic(err)
	}
	return 0
}

func workFirstErr() error { return nil }

func firstresultSilentIfConstTrue() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	if true {
		<-ch
		<-ch
		<-ch
	} else {
		return 0
	}
	return 0
}

func firstresultSilentSelectDrainCounted() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	sum := 0
	for range 3 {
		select {
		case v := <-ch:
			sum += v
		}
	}
	return sum
}

func firstresultSilentSwitchBalanced(c int) int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	switch c {
	case 0:
		<-ch
		<-ch
		<-ch
	case 1:
		<-ch
		<-ch
		<-ch
	default:
		<-ch
		<-ch
		<-ch
	}
	return 0
}

func firstresultSilentSwitchConst() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	switch 0 {
	case 0:
		<-ch
		<-ch
		<-ch
	default:
		<-ch
		<-ch
		<-ch
	}
	return 0
}


func firstresultSilentTypeSwitchBalanced(x any) int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	switch x.(type) {
	case int:
		<-ch
		<-ch
		<-ch
	default:
		<-ch
		<-ch
		<-ch
	}
	return 0
}


func firstresultSilentSelectBalanced() int {
	ch := make(chan int)
	done := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	select {
	case v := <-ch:
		_ = v
		<-ch
		<-ch
	case v := <-done:
		_ = v
		<-ch
		<-ch
		<-ch
	}
	return 0
}


func firstresultSilentIfElseOsExit() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	if err := workFirstErr(); err == nil {
		a := <-ch
		b := <-ch
		c := <-ch
		_ = a + b + c
	} else {
		os.Exit(1)
	}
	return 0
}

func firstresultSilentIfElseLogFatal() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	if err := workFirstErr(); err == nil {
		a := <-ch
		b := <-ch
		c := <-ch
		_ = a + b + c
	} else {
		log.Fatal(err)
	}
	return 0
}
func firstresultSilentSwitchConstDefault() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	switch 1 {
	case 0:
		<-ch
		<-ch
		<-ch
	default:
		<-ch
		<-ch
		<-ch
	}
	return 0
}

func firstresultSilentSwitchConstDefaultNoReturn() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	switch 1 {
	case 0:
		<-ch
		<-ch
		<-ch
	default:
		panic("unreachable")
	}
	return 0
}


func firstresultSilentDeferRecv() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	defer useFirst(<-ch)
	<-ch
	<-ch
	return 0
}

func firstresultSilentAfterIf(debug bool) int {
	ch := make(chan int)
	for i := 0; i < 2; i++ {
		go func() { ch <- i }()
	}
	if debug {
		useFirst(1)
	}
	<-ch
	<-ch
	return 0
}
func firstresultSilentGoRecv() int {
	ch := make(chan int)
	for i := 0; i < 1; i++ {
		go func() { ch <- i }()
	}
	go useFirst(<-ch)
	return 0
}

func firstresultSilentSwitchCaseRecv() int {
	ch := make(chan int)
	for i := 0; i < 1; i++ {
		go func() { ch <- i }()
	}
	switch {
	case (<-ch) == 0:
		useFirst(0)
	}
	return 0
}

func firstresultSilentSelectDefaultExit() int {
	ch := make(chan int)
	for i := 0; i < 1; i++ {
		go func() { ch <- i }()
	}
	select {
	case v := <-ch:
		_ = v
		return 1
	default:
		log.Fatal("none")
	}
	return 0
}

func firstresultSilentHugeCap() int {
	ch := make(chan struct{}, 9223372036854775807)
	for i := 0; i < 3; i++ {
		go func() { ch <- struct{}{} }()
	}
	<-ch
	return 0
}

func firstresultSilentLabeledDrain() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	goto drain
drain:
	for j := 0; j < 3; j++ {
		<-ch
	}
	return 0
}

func firstresultSilentSwitchDefaultCaseRecv() int {
	ch := make(chan int)
	for i := 0; i < 1; i++ {
		go func() { ch <- i }()
	}
	switch {
	case <-ch == 0:
	default:
	}
	return 0
}

func firstresultSilentSplitIf(c bool) int {
	ch := make(chan int)
	if c {
		for i := 0; i < 2; i++ {
			go func() { ch <- i }()
		}
	}
	if c {
		<-ch
		<-ch
	}
	return 0
}

func firstresultSilentSelectDefaultRecv() int {
	ch := make(chan int)
	for i := 0; i < 1; i++ {
		go func() { ch <- i }()
	}
	select {
	case <-ch:
	default:
		<-ch
	}
	return 0
}

func firstresultSilentForwardGoto() int {
	ch := make(chan int)
	for i := 0; i < 1; i++ {
		go func() { ch <- i }()
	}
	goto LFirstGoto
	return 0
LFirstGoto:
	<-ch
	return 1
}

func firstresultReportedZeroTrip() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and at most 1 values are received with capacity 0`
	}
	for j := 0; j < 0; j++ {
		<-ch
	}
	return <-ch
}

func firstresultReportedRangeZero() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }() // want `3 goroutines send on ch and at most 1 values are received with capacity 0`
	}
	for range 0 {
		<-ch
	}
	return <-ch
}

func firstresultSilentBackwardGoto() int {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { ch <- i }()
	}
	n := 0
Top:
	n++
	<-ch
	if n < 3 {
		goto Top
	}
	return 0
}
