package selfwait

import "sync"

func selfwaitReported() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait() // want "no goroutine can call Done"
}

func selfwaitSilent() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
	}()
	wg.Wait()
}

func selfwaitSilentBalanced() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	wg.Wait()
}
