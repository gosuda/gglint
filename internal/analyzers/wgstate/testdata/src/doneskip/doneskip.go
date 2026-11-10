package doneskip

import (
	"sync"
	"testing"
)

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
func doneskipSilentWgGo() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Go(func() {
		if bad() {
			return
		}
		work()
		wg.Done()
	})
	wg.Wait()
}

func doneskipSilentLabeledDone() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			goto Done
		Done:
			wg.Done()
			return
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}
func doneskipSilentDoneEarlier() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		if bad() {
			return
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}
func doneskipReportedTestErrorf(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			t.Errorf("Query: %v", bad())
			return // want "so Wait blocks forever"
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}

func doneskipReportedTestError(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			t.Error(bad())
			return // want "so Wait blocks forever"
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}
func doneskipSilentBlockedDone() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			{
				wg.Done()
			}
			return
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}
