package shared

import (
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

func sharedLoopCondReported(xs []int) int {
	var mu sync.Mutex
	x := 0
	mu.Lock()
	go func() { // want `x is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		mu.Lock()
		x = 1
		mu.Unlock()
	}()
	for i := 0; x < len(xs) && i < 3; i++ {
		mu.Unlock()
	}
	_ = xs
	return 0
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
