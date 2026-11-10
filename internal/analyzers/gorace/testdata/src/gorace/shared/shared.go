package shared

import (
	"sync"
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

func sharedReturnReported() string {
	s := "a"
	go func() { // want `s is written by this goroutine and read at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		s = "b"
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

func sharedCaseReadReported() {
	s := "a"
	go func() { // want `s is read by this goroutine and written at line [0-9]+ before the goroutine is joined: pass it as an argument, or wait for the goroutine first`
		switch {
		case s == "a":
			println("x")
		}
	}()
	s = "b"
}

type sharedReg struct{ m map[int]int }

func sharedSynctestSilent() string {
	s := "a"
	go func() {
		s = "b"
	}()
	synctest.Wait()
	return s
}

func sharedFieldMapSilent() {
	r := &sharedReg{m: map[int]int{}}
	go func() {
		r.m[1] = 1
	}()
	_ = r.m[2]
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
