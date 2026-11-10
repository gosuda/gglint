package doneskip

import "sync"

func bad() bool { return false }

func work() {}

func doneskipReported() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			return // want "so Wait blocks forever"
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}

func doneskipSilent() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if bad() {
			return
		}
		work()
	}()
	wg.Wait()
}

func doneskipSilentBefore() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			wg.Done()
			return
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}
