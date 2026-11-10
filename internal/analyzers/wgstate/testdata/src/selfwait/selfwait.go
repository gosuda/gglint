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
func selfwaitSilentGoDone() {
	var wg sync.WaitGroup
	wg.Add(1)
	go wg.Done()
	wg.Wait()
}
func selfwaitReportedDeferDone() {
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()
	wg.Wait() // want "no goroutine can call Done"
}

func selfwaitSilentMethodValue() {
	var wg sync.WaitGroup
	wg.Add(1)
	done := wg.Done
	done()
	wg.Wait()
}

func selfwaitSilentMethodValueLiteral() {
	var wg sync.WaitGroup
	wg.Add(1)
	h := struct{ done func() }{done: wg.Done}
	go h.done()
	wg.Wait()
}
type selfwaitJob struct {
	wg sync.WaitGroup
}

func (j *selfwaitJob) finish() {
	j.wg.Done()
}

func selfwaitSilentStructField() {
	var j selfwaitJob
	j.wg.Add(1)
	j.finish()
	j.wg.Wait()
}

func selfwaitSilentBranchDone(bad bool) {
	var wg sync.WaitGroup
	wg.Add(1)
	if bad {
		wg.Done()
	} else {
		wg.Done()
	}
	wg.Wait()
}

func selfwaitSilentNestedAdd(n int, c bool) {
	var wg sync.WaitGroup
	if c && n > 0 {
		wg.Add(n)
		for range n {
			wg.Done()
		}
	}
	wg.Wait()
}

func selfwaitSilentHiddenAdd(c bool, n int) {
	var wg sync.WaitGroup
	if c && n > 0 {
		wg.Add(n)
		for range n {
			wg.Done()
		}
	}
	wg.Add(2)
	wg.Done()
	wg.Done()
	wg.Wait()
}

func selfwaitSilentNonConstElsewhere(c bool, n int) {
	var wg sync.WaitGroup
	if c {
		wg.Add(n)
	}
	wg.Add(1)
	return
	wg.Wait()
}

func selfwaitSilentDeferLit() {
	var wg sync.WaitGroup
	wg.Add(1)
	defer func() { wg.Done() }()
	return
	wg.Wait()
}

func selfwaitSilentAcrossIf(c bool) {
	var wg sync.WaitGroup
	wg.Add(1)
	if c {
		println("x")
	}
	return
	wg.Wait()
}

func selfwaitSilentParam(wg *sync.WaitGroup) {
	wg.Add(1)
	return
	wg.Wait()
}

func selfwaitSilentBranchDoneBefore(c bool) {
	var wg sync.WaitGroup
	if false {
		wg.Done()
	}
	wg.Add(1)
	return
	wg.Wait()
}

func selfwaitSilentGoField() {
	var c selfwaitJob
	c.wg.Add(1)
	go c.wg.Add(1)
	return
	c.wg.Wait()
}
