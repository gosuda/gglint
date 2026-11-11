package doneskip

import (
	"log"
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
			return // want "any Wait on it blocks forever"
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
	}()
	wg.Wait()
}

func doneskipReportedTestError(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			t.Error(bad())
			return // want "any Wait on it blocks forever"
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

func finish(wg *sync.WaitGroup) { wg.Done() }

func doneskipSilentHelper() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			finish(&wg)
			return
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}

func doneskipSilentLogFatal() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			log.Fatal("bad")
			return
		}
		work()
		wg.Done()
	}()
	wg.Wait()
}

func doneskipReportedWgGo() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Go(func() {
		if bad() {
			return // want "any Wait on it blocks forever"
		}
		work()
		wg.Done()
	})
	wg.Wait()
}

func doneskipSilentDeferredAndPlain() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if bad() {
			return
			wg.Done()
		}
		work()
	}()
	wg.Wait()
}

func doneskipSilentBranchDoneBefore() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if true {
			wg.Done()
		}
		if true {
			return
		}
		work()
	}()
	wg.Wait()
}

func doneskipSilentNestedDone() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if bad() {
			done := wg.Done
			done()
			return
		}
		fn := func() { wg.Done() }
		fn()
	}()
	wg.Wait()
}

func doneskipIteratorExit(next func() (int, bool), handle func(int), n int) {
	var wg sync.WaitGroup
	wg.Add(n)
	go func() {
		for {
			x, ok := next()
			if !ok {
				return
			}
			handle(x)
			wg.Done()
		}
	}()
	wg.Wait()
}

func doneskipRetryNoCarry(try func() error) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for attempt := 0; ; attempt++ {
			if err := try(); err != nil {
				if attempt == 3 {
					return // want "skips.*Done"
				}
				continue
			}
			wg.Done()
			return
		}
	}()
	wg.Wait()
}

func doneskipLoopBreakBad(next func() bool) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for {
			if !next() {
				return // want "skips.*Done"
			}
			wg.Done()
			break
		}
	}()
	wg.Wait()
}
