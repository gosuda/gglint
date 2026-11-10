package addlate

import "sync"

func work() {}

func addlateReported() {
	var wg sync.WaitGroup
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Add(1) // want "so Wait can return early"
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
	wg.Add(1) // want "so Wait can return early"
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
	wg.Add(2) // want "so Wait can return early"
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
func addlateSilentWrongGate() {
	var wg sync.WaitGroup
	start := make(chan struct{})
	other := make(chan struct{})
	go func() {
		<-other
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	close(start)
	wg.Wait()
}
func addlateSilentCloseBefore() {
	var wg sync.WaitGroup
	start := make(chan struct{})
	go func() {
		<-start
		defer wg.Done()
		work()
	}()
	close(start)
	wg.Add(1)
	wg.Wait()
}
func addlateSilentRLockGate() {
	var wg sync.WaitGroup
	var mu sync.RWMutex
	go func() {
		mu.RLock()
		defer wg.Done()
		mu.RUnlock()
		work()
	}()
	mu.RLock()
	wg.Add(1)
	mu.RUnlock()
	wg.Wait()
}
func addlateSilentLockAfter() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	go func() {
		mu.Lock()
		defer wg.Done()
		mu.Unlock()
		work()
	}()
	wg.Add(1)
	mu.Lock()
	mu.Unlock()
	wg.Wait()
}
func addlateSilentBranchGate(ok bool) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	go func() {
		if ok {
			<-start
		}
		defer wg.Done()
		work()
	}()
	wg.Add(1)
	close(start)
	wg.Wait()
}
func addlateSilentDeferredGate() {
	var wg sync.WaitGroup
	start := make(chan struct{})
	go func() {
		defer wg.Done()
		<-start
		work()
	}()
	wg.Add(1)
	close(start)
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
