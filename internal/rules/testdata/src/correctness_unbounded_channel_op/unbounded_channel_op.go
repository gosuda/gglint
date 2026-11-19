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
	case a <- 1:
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
	case ch <- 2:
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
	case v := <-in:
		return v
	case <-time.After(time.Second):
		return 0
	}
}

func tickRecv() {
	<-time.Tick(time.Second) // want "channel receive can block forever"
}

func waitTimeout(ch chan int, d time.Duration) (int, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-time.After(d):
		return 0, false
	}
}

func tryRecv(ch chan int) (int, bool) {
	select {
	case v := <-ch:
		return v, true
	default:
		return 0, false
	}
}

func timerLocalBound(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case v := <-in:
		return v
	case <-t.C:
		return 0
	}
}

func tickerLocalBound(in chan int, d time.Duration) int {
	t := time.NewTicker(d)
	defer t.Stop()
	select {
	case v := <-in:
		return v
	case <-t.C:
		return 0
	}
}

func timerParamUnproven(in chan int, t *time.Timer) int {
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

func timerParamAssignedUnproven(in chan int, t *time.Timer) int {
	t = time.NewTimer(time.Second)
	defer t.Stop()
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

func timerAfterFuncUnproven(in chan int, d time.Duration) int {
	t := time.AfterFunc(d, func() {})
	defer t.Stop()
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

func timerStoppedUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	t.Stop()
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

func timerLoopUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	for range 3 {
		select {
		case v := <-in: // want "channel receive can block forever"
			return v
		case <-t.C: // want "channel receive can block forever"
			return 0
		}
	}
	return 0
}

func timerCondResetUnproven(in chan int, d time.Duration, c bool) int {
	t := time.NewTimer(d)
	defer t.Stop()
	for range 3 {
		if c {
			t.Reset(d)
		}
		select {
		case v := <-in: // want "channel receive can block forever"
			return v
		case <-t.C: // want "channel receive can block forever"
			return 0
		}
	}
	return 0
}

func timerAliasUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	u := t
	u.Stop()
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

func timerDerefStopUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	(*t).Stop()
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

func tickerLoopBound(in chan int, d time.Duration) int {
	t := time.NewTicker(d)
	defer t.Stop()
	for range 3 {
		select {
		case v := <-in:
			return v
		case <-t.C:
			return 0
		}
	}
	return 0
}

func timerResetBound(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	for range 3 {
		t.Reset(d)
		select {
		case v := <-in:
			return v
		case <-t.C:
			return 0
		}
	}
	return 0
}

func timerResetArgRecv(in chan int, d time.Duration, ch chan int64) int {
	t := time.NewTimer(d)
	defer t.Stop()
	for range 3 {
		t.Reset(time.Duration(<-ch)) // want "channel receive can block forever"
		select {
		case v := <-in:
			return v
		case <-t.C:
			return 0
		}
	}
	return 0
}

func timerNoDeferBound(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	select {
	case v := <-in:
		return v
	case <-t.C:
		return 0
	}
}

func tickerChanOverwriteUnproven(in chan int, d time.Duration) int {
	t := time.NewTicker(d)
	defer t.Stop()
	t.C = nil
	select {
	case <-in: // want "channel receive can block forever"
		return 1
	case <-t.C: // want "channel receive can block forever"
		return 0
	}
}

type timerBox struct{ t *time.Timer }

func arm(t *time.Timer) {}

func timerEscapedUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	arm(t)
	select {
	case <-t.C: // want "channel receive can block forever"
		return 1
	case v := <-in: // want "channel receive can block forever"
		return v
	}
}

func timerFieldEscapeUnproven(in chan int, d time.Duration, b *timerBox) int {
	t := time.NewTimer(d)
	b.t = t
	select {
	case <-t.C: // want "channel receive can block forever"
		return 1
	case v := <-in: // want "channel receive can block forever"
		return v
	}
}

func timerSendEscapeUnproven(in chan int, d time.Duration, ts chan *time.Timer) int {
	t := time.NewTimer(d)
	ts <- t // want "channel send can block forever"
	select {
	case <-t.C: // want "channel receive can block forever"
		return 1
	case v := <-in: // want "channel receive can block forever"
		return v
	}
}

func timerReturnEscapeUnproven(in chan int, d time.Duration) (int, *time.Timer) {
	t := time.NewTimer(d)
	select {
	case <-t.C: // want "channel receive can block forever"
		return 1, t
	case v := <-in: // want "channel receive can block forever"
		return v, t
	}
}

func timerLiteralEscapeUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	func() { t.Reset(d) }()
	select {
	case <-t.C: // want "channel receive can block forever"
		return 1
	case v := <-in: // want "channel receive can block forever"
		return v
	}
}

func timerLoopResetBound(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-in:
	case <-t.C:
	}
	for range 3 {
		t.Reset(d)
		select {
		case v := <-in:
			return v
		case <-t.C:
			return 0
		}
	}
	return 0
}

func timerGotoBackUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
retry:
	select {
	case v := <-in: // want "channel receive can block forever"
		return v
	case <-t.C: // want "channel receive can block forever"
		goto retry
	}
}

func timerGotoBackRecreatedBound(in chan int, d time.Duration) int {
retry:
	t := time.NewTimer(d)
	select {
	case v := <-in:
		return v
	case <-t.C:
		goto retry
	}
}

func tickerGotoBackBound(in chan int, d time.Duration) int {
	t := time.NewTicker(d)
	defer t.Stop()
retry:
	select {
	case v := <-in:
		return v
	case <-t.C:
		goto retry
	}
}

func timerGotoForwardBound(in chan int, d time.Duration, skip bool) int {
	t := time.NewTimer(d)
	defer t.Stop()
	if skip {
		goto done
	}
	select {
	case v := <-in:
		return v
	case <-t.C:
	}
done:
	return 0
}

func timerGotoBackLoopResetBound(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	rounds := 0
retry:
	for range 3 {
		t.Reset(d)
		select {
		case v := <-in:
			return v
		case <-t.C:
		}
	}
	if rounds < 2 {
		rounds++
		goto retry
	}
	return 0
}

func timerGotoBackPastResetUnproven(in chan int, d time.Duration) int {
	t := time.NewTimer(d)
	defer t.Stop()
	for range 3 {
		t.Reset(d)
	again:
		select {
		case v := <-in: // want "channel receive can block forever"
			return v
		case <-t.C: // want "channel receive can block forever"
			goto again
		}
	}
	return 0
}
