package shared

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"golang.org/x/sync/errgroup"
)

func workErr() error { return nil }

func sharedReported() {
	s := "a"
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		println(s)
	}()
	s = "b"
}

func sharedSilent() {
	s1 := "a"
	done := make(chan struct{})
	go func() {
		defer close(done)
		println(s1)
	}()
	<-done
	s1 = "b"
	_ = s1
	var err error
	var g errgroup.Group
	g.Go(func() error {
		err = workErr()
		return nil
	})
	_ = g.Wait()
	_ = err
	s3 := "a"
	_ = s3
	go func() {
		s3 = "c"
	}()
}

func sharedWriteReadReported() string {
	s := "a"
	_ = s
	go func() { // want `s is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		s = "c"
	}()
	return s
}

func sharedGuardedSilent() {
	var mu sync.Mutex
	s := 0
	go func() {
		mu.Lock()
		s = 1
		mu.Unlock()
	}()
	mu.Lock()
	s = 2
	println(s)
	mu.Unlock()
}

func sharedCondSilent() {
	s := "a"
	done := make(chan struct{})
	xs := []int{1}
	go func() {
		s = "b"
		close(done)
	}()
	<-done
	if s == "b" {
		println("x")
	}
	for s == "" {
		break
	}
	if len(xs) == 1 {
		println("y")
	}
	for _, v := range xs {
		_ = v
	}
	_ = s
}

func sharedIncSilent() {
	s := 0
	done := make(chan struct{})
	go func() {
		s = 1
		close(done)
	}()
	<-done
	s++
	_ = s
}

func sharedWgSilent(wg *sync.WaitGroup) {
	s := 0
	wg.Add(1)
	go func(w *sync.WaitGroup, v int) {
		defer w.Done()
		_ = v
	}(wg, s)
	wg.Wait()
	_ = s
}

func sharedEarliestJoinedReported(c bool) {
	s := "a"
	done := make(chan struct{})
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		defer close(done)
		println(s)
	}()
	if c {
		<-done
		s = "x"
	}
	s = "y"
}

func sharedUnguardedAfterReadReported() {
	var mu sync.Mutex
	s := 0
	go func() { // want `s is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		mu.Lock()
		_ = s
		mu.Unlock()
		s = 1
	}()
	mu.Lock()
	println(s)
	mu.Unlock()
}

func sharedReadThenGuardedReported() {
	var mu sync.Mutex
	s := 0
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		_ = s
		mu.Lock()
		s = 1
		mu.Unlock()
	}()
	mu.Lock()
	s = 2
	mu.Unlock()
}

func sharedSecondWriteReported() {
	var mu sync.Mutex
	s := 0
	go func() { // want `s is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		mu.Lock()
		s = 1
		mu.Unlock()
		s = 2
	}()
	mu.Lock()
	println(s)
	mu.Unlock()
}

func sharedCondReadReported() {
	s := "a"
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		if s == "a" {
			println("x")
		}
	}()
	s = "b"
}

func sharedSynctestSilent(t *testing.T) string {
	var out string
	synctest.Test(t, func(*testing.T) {
		s := "a"
		go func() {
			s = "b"
		}()
		synctest.Wait()
		out = s
	})
	return out
}

func sharedIndexReadReported(m map[int]int) {
	k := 0
	go func() { // want `k is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		if m[k] > 0 {
			println("x")
		}
	}()
	k = 1
}

func sharedIfGuardedSilent() {
	var mu sync.Mutex
	n := 0
	go func() {
		mu.Lock()
		if n > 0 {
			println("pos")
		}
		mu.Unlock()
	}()
	mu.Lock()
	n = 2
	mu.Unlock()
}

func sharedDisjointSilent() {
	out := make([]int, 2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		out[0] = 1
	}()
	out[1] = 2
	wg.Wait()
}

func sharedSameIndexReported() {
	out := make([]int, 2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // want `out is written by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		defer wg.Done()
		out[0] = 1
	}()
	out[0] = 2
	wg.Wait()
}

func sharedMutexHandoffSilent() int {
	var mu sync.Mutex
	x := 0
	mu.Lock()
	go func() {
		x = 1
		mu.Unlock()
	}()
	mu.Lock()
	defer mu.Unlock()
	return x
}

type sharedGate chan struct{}

func (g sharedGate) open() { g <- struct{}{} }

func (g sharedGate) wait() { <-g }

func sharedGateHelperSilent() bool {
	set, g := false, make(sharedGate, 1)
	go func() {
		set = true
		g.open()
	}()
	g.wait()
	return set
}

func sharedSettle() { synctest.Wait() }

func sharedSynctestHelperSilent(t *testing.T) int {
	var got int
	synctest.Test(t, func(*testing.T) {
		go func() { got = 1 }()
		sharedSettle()
	})
	return got
}

func sharedPingCloseSilent(ready chan struct{}) error {
	var perr error
	errc := make(chan struct{})
	go func() {
		if perr = workErr(); perr != nil {
			close(errc)
			return
		}
	}()
	select {
	case <-errc:
		return perr
	case <-ready:
		return nil
	}
}

func sharedCallArgSendSilent() int {
	ready := make(chan struct{})
	gp := 0
	go func() {
		gp = 1
		sharedCall(func() { ready <- struct{}{} })
	}()
	<-ready
	return gp
}

func sharedCall(f func()) { f() }

type sharedAtomicGate struct{ set chan struct{} }

func (g *sharedAtomicGate) unlock() { g.set <- struct{}{} }

func (g *sharedAtomicGate) wait() { <-g.set }

func sharedMethodHelperSilent(g *sharedAtomicGate) bool {
	set := false
	go func() {
		set = true
		g.unlock()
	}()
	g.wait()
	return set
}

func sharedRendezvousSilent() int {
	var count int
	c := make(chan byte)
	go func() {
		<-c
		count++
		<-c
	}()
	c <- 1
	c <- 2
	return count
}

func sharedBufferedSendReported() int {
	var count int
	c := make(chan byte, 1)
	go func() { // want `count is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		<-c
		count++
		<-c
	}()
	c <- 1
	c <- 2
	return count
}

func sharedUnrelatedCallReported() string {
	s := "a"
	done := make(chan struct{}, 1)
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		println(s)
		done <- struct{}{}
	}()
	_ = workErr()
	s = "b"
	return s
}

func sharedOtherLockReported() string {
	var mu sync.Mutex
	var other sync.Mutex
	s := "a"
	mu.Lock()
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		println(s)
		mu.Unlock()
	}()
	other.Lock()
	s = "b"
	other.Unlock()
	mu.Lock()
	defer mu.Unlock()
	return s
}

func sharedNestedRecvBufferedReported() int {
	ready := make(chan struct{}, 1)
	gp := 0
	go func() { // want `gp is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		gp = 1
		sharedCall(func() { <-ready })
	}()
	ready <- struct{}{}
	return gp
}

func sharedNestedCloseSilent(wg *sync.WaitGroup, done chan struct{}) int {
	gp := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		gp = 1
		sharedCall(func() { close(done) })
	}()
	<-done
	wg.Wait()
	return gp
}

func sharedParamRendezvousSilent() int {
	var count int
	c := make(chan byte)
	go func(c chan byte) {
		<-c
		count++
		count++
		<-c
	}(c)
	c <- 1
	c <- 2
	return count
}

type sharedGate2 struct{ done chan struct{} }

func (g *sharedGate2) wait2() { <-g.done }

func sharedGateCallSilent() int {
	g := &sharedGate2{done: make(chan struct{})}
	x := 0
	go func() {
		x = 1
		close(g.done)
	}()
	g.wait2()
	return x
}

type sharedWaiter2 interface{ wait2() }

func sharedGateInterfaceSilent() int {
	g := &sharedGate2{done: make(chan struct{})}
	var w sharedWaiter2 = g
	x := 0
	go func() {
		x = 1
		close(g.done)
	}()
	w.wait2()
	return x
}

func sharedSelectReturnSilent(ctx context.Context) (int, error) {
	x := 0
	done := make(chan struct{})
	go func() {
		x = 1
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	return x, nil
}

func sharedSingleSelectSilent() int {
	x := 0
	done := make(chan struct{})
	go func() {
		x = 1
		select {
		case done <- struct{}{}:
		}
	}()
	select {
	case <-done:
	}
	return x
}

type sharedHolder struct {
	done chan struct{}
	name string
}

func sharedSiblingFieldReported() int {
	x := 0
	g := &sharedHolder{done: make(chan struct{})}
	g.name = "a"
	_ = g.name
	go func() { // want `x is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		x = 1
		close(g.done)
	}()
	fmt.Println(g.name)
	return x
}

func sharedBoundLitSilent() int {
	x := 0
	done := make(chan struct{})
	f := func() { close(done) }
	wait := func() { <-done }
	go func() {
		x = 1
		f()
	}()
	wait()
	return x
}

func sharedRangeSilent() int {
	x := 0
	ch := make(chan struct{})
	go func() {
		x = 1
		close(ch)
	}()
	for range ch {
		break
	}
	return x
}

func sharedGateAssignSilent() int {
	g := &sharedGate2{done: make(chan struct{})}
	var w sharedWaiter2
	w = g
	x := 0
	go func() {
		x = 1
		close(g.done)
	}()
	w.wait2()
	return x
}

func sharedDeclRecvSilent() int {
	x := 0
	done := make(chan struct{})
	go func() {
		x = 1
		close(done)
	}()
	_ = <-done
	return x
}

func sharedSelectEscapeReported(ready chan struct{}) (int, int) {
	conn := 0
	err := 0
	done := make(chan struct{}, 1)
	go func() { // want `conn is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		conn = 1
		err = 1
		close(done)
		_ = done
	}()
	select {
	case <-ready:
		err = 2
	case <-done:
	}
	return conn, err
}

func sharedSemSilent() int {
	sem := make(chan struct{}, 1)
	var wg sync.WaitGroup
	x := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		sem <- struct{}{}
		x++
		<-sem
	}()
	sem <- struct{}{}
	x++
	<-sem
	wg.Wait()
	return x
}

func sharedStartGateSilent() int {
	start := make(chan struct{})
	var wg sync.WaitGroup
	x := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_ = x
	}()
	x = 1
	close(start)
	wg.Wait()
	return x
}

func sharedStartGateReorderedReported() int {
	start := make(chan struct{})
	x := 0
	go func() { // want `x is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		<-start
		_ = x
	}()
	close(start)
	x = 1
	return x
}

func sharedWaitInOtherGoroutineReported() int {
	x := 0
	done := make(chan struct{})
	go func() { // want `x is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		x = 1
		close(done)
	}()
	go func() { <-done }()
	return x
}

func sharedStoredWaitReported() int {
	x := 0
	done := make(chan struct{})
	go func() { // want `x is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		x = 1
		close(done)
	}()
	wait := func() { <-done }
	_ = wait
	return x
}

func sharedGoCallArgWaitReported() int {
	x := 0
	done := make(chan struct{})
	go func() { // want `x is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		x = 1
		close(done)
	}()
	go sharedCall(func() { <-done })
	return x
}

func sharedWaitFor(f func()) int { f(); return 0 }

func sharedSink(int) {}

func sharedDrainThenWaitSilent() {
	x := 0
	sig := make(chan struct{})
	spare := make(chan int, 1)
	go func() {
		select {
		case <-spare:
		default:
		}
		<-sig
		x = 2
	}()
	x = 1
	_ = x
	sig <- struct{}{}
}

func sharedDeferredSendSilent() {
	x := 0
	done := make(chan struct{})
	go func() {
		<-done
		x = 1
	}()
	defer func() { done <- struct{}{} }()
	x = 2
	_ = x
}

func sharedInPlaceCloseSilent() int {
	x := 0
	done := make(chan struct{})
	go func() {
		x = 1
		func() { close(done) }()
	}()
	<-done
	return x
}

func sharedGoArgCallWaitsSilent() int {
	x := 0
	ready := make(chan struct{})
	go func() { x = 1; close(ready) }()
	go sharedSink(sharedWaitFor(func() { <-ready }))
	return x
}

func sharedGoArgBoundCallWaitsSilent() int {
	x := 0
	done := make(chan struct{})
	go func() { x = 1; close(done) }()
	wait := func() int { <-done; return 0 }
	go sharedSink(wait())
	return x
}

func sharedSelectBodyWaitSilent(other chan int) int {
	x := 0
	sig := make(chan struct{})
	go func() {
		x = 1
		close(sig)
		_ = 0
	}()
	select {
	case <-sig:
	case v := <-other:
		_ = v
		<-sig
	}
	return x
}

func sharedCondEntryWaitReported(c bool) {
	x := 0
	_ = x
	sig := make(chan struct{})
	go func() { // want `x is written by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		if c {
			<-sig
		}
		x = 1
	}()
	x = 2
	close(sig)
}

func sharedPreGoWaitReported(c bool, sig chan struct{}) int {
	if c {
		<-sig
	}
	x := 0
	go func() { // want `x is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		x = 1
		close(sig)
	}()
	return x
}

func sharedEntryAssignWaitSilent(ch chan int) int {
	n := 0
	done := make(chan struct{})
	go func() {
		v := <-ch
		n = v
		close(done)
	}()
	n = 5
	ch <- 1
	<-done
	return n
}
