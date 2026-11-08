// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/time_after_in_loop_go.go.

// errorcheck -0

// perf::time_after_in_loop on a Go package (R-U1): logos lint applies the
// rule to Go syntax and Go types alone, so the Go twin of the loGo/s attack
// file reports the same loops.
//
// correctness::unbounded_channel_op also reports the channel receives of this
// file. They are not its subject, so each carries //logos:allow for that rule
// and the file checks perf::time_after_in_loop alone;
// test/logos/lint/unbounded_channel_op_go.go covers the rule.

package p

import "time"

type clock struct{}

func (clock) After(d time.Duration) <-chan time.Time { return nil }

func After(d time.Duration) <-chan time.Time { return nil }

func selectLoop(d time.Duration, ch <-chan int) {
	for {
		select {
		case <-ch:
			return
		case <-time.After(d): // want `time\.After in a loop allocates a timer per iteration`
		}
	}
}

func threeClause(d time.Duration, n int) {
	for i := 0; i < n; i++ {
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
	}
}

func rangeLoop(d time.Duration, xs []int) {
	for range xs {
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
	}
	for i := range 3 {
		_ = i
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
	}
}

func conditionLoop(d time.Duration, ok func() bool) {
	for ok() {
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
	}
}

func nestedLoops(d time.Duration) {
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
		}
		<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
	}
}

func closureInLoop(d time.Duration) {
	for i := 0; i < 2; i++ {
		func() {
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
		}()
	}
}

func loopInClosure(d time.Duration) func() {
	return func() {
		for {
			<-time.After(d) // want `time\.After in a loop allocates a timer per iteration`
		}
	}
}

func twoCallsOneBody(d time.Duration) {
	for {
		a := time.After(d) // want `time\.After in a loop allocates a timer per iteration`
		b := time.After(d) // want `time\.After in a loop allocates a timer per iteration`
		_, _ = a, b
	}
}

func drain(c <-chan time.Time) { <-c }

func argumentInLoop(d time.Duration) {
	for i := 0; i < 2; i++ {
		drain(time.After(d)) // want `time\.After in a loop allocates a timer per iteration`
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
}

func notTimeAfter(d time.Duration) {
	var c clock
	now := time.Now()
	for i := 0; i < 2; i++ {
		_ = now.After(now)
		<-c.After(d)
		<-After(d)
		_ = time.Tick
		f := time.After
		_ = f
	}
}
