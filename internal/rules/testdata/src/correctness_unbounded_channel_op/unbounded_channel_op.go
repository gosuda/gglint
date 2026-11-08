// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/unbounded_channel_op_go.go.

// errorcheck

package p

import (
	"context"
	"time"
)

type hub struct{ ch chan int }

type task struct{ done chan struct{} }

func (t task) Done() <-chan struct{} { return t.done }

const slots = 2

func paramSend(ch chan int) {
	ch <- 1 // want "channel send can block forever"
}

func paramRecv(ch chan int) int {
	return <-ch // want "channel receive can block forever"
}

func assignRecv(ch chan int) int {
	v := <-ch // want "channel receive can block forever"
	return v
}

func commaOkRecv(ch chan int) bool {
	_, ok := <-ch // want "channel receive can block forever"
	return ok
}

func statementRecv(ch chan int) {
	<-ch // want "channel receive can block forever"
}

func fieldSend(h *hub) {
	h.ch <- 1 // want "channel send can block forever"
}

func indexSend(chs []chan int) {
	chs[0] <- 1 // want "channel send can block forever"
}

func selectNoDone(a, b chan int) int {
	select {
	case v := <-a: // want "channel receive can block forever"
		return v
	case b <- 1: // want "channel send can block forever"
		return 0
	}
}

func selectDefault(a chan int) {
	select {
	case a <- 1: // want "channel send can block forever"
	default:
	}
}

func bodyInDoneSelect(ctx context.Context, in, out chan int) {
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-in:
			out <- v // want "channel send can block forever"
		}
	}
}

func doneSelect(ctx context.Context, in chan int) int {
	select {
	case <-ctx.Done():
		return 0
	case v := <-in:
		return v
	}
}

func doneSelectSend(ctx context.Context, out chan int) {
	select {
	case out <- 1:
	case <-ctx.Done():
	}
}

func doneAssign(ctx context.Context, in chan int) {
	select {
	case _, ok := <-ctx.Done():
		_ = ok
	case <-in:
	}
}

func customDone(t task, in chan int) int {
	select {
	case <-t.Done():
		return 0
	case v := <-in:
		return v
	}
}

func waitDone(ctx context.Context) {
	<-ctx.Done()
}

func belowCap() int {
	ch := make(chan int, 2)
	ch <- 1
	return <-ch
}

func atCap() int {
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	return <-ch + <-ch
}

func overCap() int {
	ch := make(chan int, 2)
	ch <- 1     // want "channel send can block forever"
	ch <- 2     // want "channel send can block forever"
	ch <- 3     // want "channel send can block forever"
	return <-ch // want "channel receive can block forever"
}

func zeroCap() int {
	ch := make(chan int, 0)
	ch <- 1     // want "channel send can block forever"
	return <-ch // want "channel receive can block forever"
}

func unbufferedNoSend() int {
	ch := make(chan int)
	return <-ch
}

func sendInLoop() {
	ch := make(chan int, 2)
	for i := 0; i < 2; i++ {
		ch <- i // want "channel send can block forever"
	}
	<-ch // want "channel receive can block forever"
}

func sendInRange() {
	ch := make(chan int, 2)
	for _, v := range []int{1, 2} {
		ch <- v // want "channel send can block forever"
	}
}

func recvInLoop() {
	ch := make(chan int, 2)
	ch <- 1
	for i := 0; i < 2; i++ {
		<-ch
	}
}

func dynamicCap(n int) {
	ch := make(chan int, n)
	ch <- 1 // want "channel send can block forever"
}

func constCap() {
	ch := make(chan int, slots)
	ch <- 1
	ch <- 2
}

func exprCap() {
	ch := make(chan int, 1+1)
	ch <- 1
	ch <- 2
}

func reassignedChan() {
	ch := make(chan int, 2)
	ch = make(chan int, 2)
	ch <- 1 // want "channel send can block forever"
}

func closureOver() {
	ch := make(chan int, 2)
	ch <- 1 // want "channel send can block forever"
	ch <- 2 // want "channel send can block forever"
	func() {
		ch <- 3 // want "channel send can block forever"
	}()
}

func closureWithin() {
	ch := make(chan int, 2)
	ch <- 1
	func() {
		ch <- 2
	}()
}

func closureInLoop() {
	ch := make(chan int, 4)
	for i := 0; i < 2; i++ {
		func() {
			ch <- i // want "channel send can block forever"
		}()
	}
}

func selectCounts() {
	ch := make(chan int, 1)
	ch <- 1 // want "channel send can block forever"
	select {
	case ch <- 2: // want "channel send can block forever"
	default:
	}
}

func varDecl() {
	var ch = make(chan int, 1)
	ch <- 1
}

func varDeclType() {
	var ch chan int = make(chan int, 1)
	ch <- 1
}

func declNoMake() {
	var ch chan int
	ch <- 1 // want "channel send can block forever"
}

func rangeChan(ch chan int) int {
	n := 0
	for v := range ch {
		n += v
	}
	return n
}

func closeAndLen(ch chan int) int {
	close(ch)
	return len(ch)
}

func timerRecv() {
	<-time.After(time.Second) // want "channel receive can block forever"
}

func timerSelect(in chan int) int {
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-time.After(time.Second): // want "channel receive can block forever"
		return 0
	}
}

func tickRecv() {
	<-time.Tick(time.Second) // want "channel receive can block forever"
}
