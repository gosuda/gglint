package timeafter

import "time"

type clock struct{}

func (clock) After(d time.Duration) <-chan time.Time { return nil }

func After(d time.Duration) <-chan time.Time { return nil }

func drain(c <-chan time.Time) { <-c }

func selectInFor(d time.Duration, ch <-chan int) {
	for {
		select {
		case <-ch:
			return
		case <-time.After(d): // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		}
	}
}

func threeClause(d time.Duration, n int) {
	for i := 0; i < n; i++ {
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
	}
}

func conditionLoop(d time.Duration, ok func() bool) {
	for ok() {
		if ok() {
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		}
	}
}

func rangeLoop(d time.Duration, xs []int) {
	for _, x := range xs {
		_ = x
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
	}
	for i := range 3 {
		_ = i
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
	}
}

func nestedLoops(d time.Duration) {
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			_ = i + j
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		}
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
	}
}

func argumentInLoop(d time.Duration) {
	for range []int{1, 2} {
		drain(time.After(d)) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
	}
}

func twoCalls(d time.Duration) (<-chan time.Time, <-chan time.Time) {
	for {
		return time.After(d), time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it` `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
	}
}

func twoCallsOneBody(d time.Duration) {
	for {
		a := time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		b := time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		_, _ = a, b
	}
}

func loopInLiteral(d time.Duration) func() {
	return func() {
		for {
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		}
	}
}

func literalInLoop(d time.Duration) {
	for i := 0; i < 2; i++ {
		func() {
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
		}()
	}
}

func literalInHeaderInLoop(d time.Duration) {
	for range 2 {
		for f := func() { <-time.After(d) }; ; { // want `time\.After in a loop allocates a timer per iteration: create one time\.Timer before the loop and Reset it`
			f()
			break
		}
	}
}

func literalOutsideLoop(d time.Duration) func() {
	return func() {
		<-time.After(d)
	}
}

func outsideLoop(d time.Duration) {
	<-time.After(d)
	t := time.NewTimer(d)
	for i := 0; i < 3; i++ {
		t.Reset(d)
		<-t.C
	}
	_ = time.AfterFunc(d, func() {})
}

func headerRunsOnce(d time.Duration) {
	for t := time.After(d); ; {
		<-t
		break
	}
	for range time.After(d) {
		break
	}
	for c := time.After(d); c != time.After(d); drain(time.After(d)) {
		break
	}
}

func notTimeAfter(d time.Duration) {
	var c clock
	now := time.Now()
	for i := 0; i < 2; i++ {
		_ = now.After(now)
		_ = c.After(d)
		_ = After(d)
		_ = time.Tick(d)
		_ = time.Now().After(time.Time{})
		f := time.After
		_ = f
	}
}
