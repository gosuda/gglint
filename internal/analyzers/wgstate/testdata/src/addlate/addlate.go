package addlate

import "sync"

func work() {}

func addlateReported() {
	var wg sync.WaitGroup
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(1) // want "negative WaitGroup counter"
	wg.Wait()
}

func addlateSilent() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Wait()
}
func addlateReportedLabeled() {
	var wg sync.WaitGroup
	if false {
		goto L
	}
	go func() {
		defer wg.Done()
		work()
	}()
L:
	wg.Add(1) // want "negative WaitGroup counter"
	wg.Wait()
}
func addlateReportedDouble() {
	var wg sync.WaitGroup
	go func() {
		defer wg.Done()
		work()
	}()
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(2) // want "negative WaitGroup counter"
	wg.Wait()
}
func addlateSilentChanGate() {
	var wg sync.WaitGroup
	start := make(chan struct{})
	go func() {
		<-start
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	close(start)
	wg.Wait()
}
func addlateSilentMutexGate() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	mu.Lock()
	go func() {
		mu.Lock()
		defer wg.Done()
		mu.Unlock()
		work()
	}()
	wg.Add(1)
	mu.Unlock()
	wg.Wait()
}
func addlateSilentRWMutexParent() {
	var wg sync.WaitGroup
	var mu sync.RWMutex
	mu.RLock()
	go func() {
		mu.Lock()
		defer wg.Done()
		mu.Unlock()
		work()
	}()
	wg.Add(1)
	mu.RUnlock()
	wg.Wait()
}
func addlateSilentSelectGate() {
	var wg sync.WaitGroup
	start := make(chan struct{})
	other := make(chan struct{})
	go func() {
		select {
		case <-start:
		case <-other:
		}
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	close(start)
	wg.Wait()
}
func addlateSilentRangeRecv() {
	var wg sync.WaitGroup
	start := make(chan int)
	go func() {
		defer wg.Done()
		for range start {
			work()
		}
	}()
	wg.Add(1)
	close(start)
	wg.Wait()
}
func addlateSilentWaitGroupGate() {
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(1)
	go func() {
		ready.Wait()
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	ready.Done()
	wg.Wait()
}
func addlateSilentCondGate() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	ready := false
	cond := sync.NewCond(&mu)
	go func() {
		defer wg.Done()
		mu.Lock()
		for !ready {
			cond.Wait()
		}
		mu.Unlock()
		work()
	}()
	wg.Add(1)
	mu.Lock()
	ready = true
	mu.Unlock()
	cond.Broadcast()
	wg.Wait()
}
func addlateReportedWaitFirst() {
	var wg sync.WaitGroup
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Wait()
	wg.Add(1) // want "can return early"
}
func addlateSilentRLockGoroutine() {
	var wg sync.WaitGroup
	var mu sync.RWMutex
	mu.Lock()
	go func() {
		mu.RLock()
		defer wg.Done()
		mu.RUnlock()
		work()
	}()
	wg.Add(1)
	mu.Unlock()
	wg.Wait()
}

func addlateSilentEarlyAndLate() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	wg.Done()
	wg.Wait()
}

func addlateSilentOtherKey() {
	var wg1, wg2 sync.WaitGroup
	go func() {
		wg1.Add(1)
		defer wg1.Done()
		work()
	}()
	wg2.Add(1)
	wg2.Done()
	wg2.Wait()
}

func addlateSilentNestedDone() {
	var wg sync.WaitGroup
	go func() {
		cleanup := func() { wg.Done() }
		_ = cleanup
		work()
	}()
	wg.Add(1)
	wg.Done()
	wg.Wait()
}

func addlateReportedLitAddNoOrder() {
	var wg sync.WaitGroup
	setup := func() {
		wg.Add(1)
	}
	_ = setup
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(1) // want "negative WaitGroup counter"
	wg.Wait()
}

func addlateSilentLitCall() {
	var wg sync.WaitGroup
	add := func() {
		wg.Add(1)
	}
	add()
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	wg.Done()
	wg.Wait()
}

func addlateSilentLitInPlace() {
	var wg sync.WaitGroup
	func() {
		wg.Add(1)
	}()
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	wg.Done()
	wg.Wait()
}
