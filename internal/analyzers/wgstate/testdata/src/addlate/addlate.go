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
