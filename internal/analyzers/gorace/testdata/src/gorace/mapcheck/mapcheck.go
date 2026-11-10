package mapcheck

import "sync"

func mapReported() {
	m := map[int]int{}
	for i := range 8 {
		go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
			m[i] = i
		}()
	}
}

func mapSilent() {
	m := map[int]int{}
	var mu sync.Mutex
	for i := range 8 {
		go func() {
			mu.Lock()
			m[i] = i
			mu.Unlock()
		}()
	}
}

func mapParentReported() {
	m := map[int]int{}
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		m[0] = 1
	}()
	m[1] = 2
}

func mapJoinedSilent() {
	m := map[int]int{}
	done := make(chan struct{})
	go func() {
		m[0] = 1
		close(done)
	}()
	<-done
	_ = m[0]
}

func mapOtherReported() {
	m := map[int]int{}
	var mu sync.Mutex
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		m[1] = 1
	}()
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		mu.Lock()
		m[2] = 2
		mu.Unlock()
	}()
	_ = mu
}

func mapKeepRefSilent(m map[int]int) {
	go func() {
		m[1] = 1
	}()
	go func() {
		keepMap(m)
	}()
}

func keepMap(m map[int]int) {}

func mapDeleteReported() {
	m := map[int]int{1: 1}
	for i := range 8 {
		go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
			delete(m, i)
		}()
	}
}

func mapIncSilent() {
	m := map[int]int{}
	var mu sync.Mutex
	for i := range 8 {
		go func() {
			mu.Lock()
			m[i]++
			mu.Unlock()
		}()
	}
}

func mapGuardedParentReported() {
	m := map[int]int{}
	var mu sync.Mutex
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		mu.Lock()
		m[1] = 1
		mu.Unlock()
	}()
	m[2] = 2
}

func mapTwoLocksReported() {
	m := map[int]int{}
	var mu1, mu2 sync.Mutex
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		mu1.Lock()
		m[1] = 1
		mu1.Unlock()
	}()
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		mu2.Lock()
		m[2] = 2
		mu2.Unlock()
	}()
}

type mapReg struct{ m map[int]int }

func mapFieldReported() {
	r := &mapReg{m: map[int]int{}}
	for i := range 8 {
		go func() { // want `r.m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
			r.m[i] = i
		}()
	}
}

func mapDeclReadReported() int {
	m := map[int]int{}
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		m[1] = 1
	}()
	var v = m[1]
	return v
}

func mapFirstAccessGuardedReported() {
	m := map[int]int{0: 0}
	var mu sync.Mutex
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		mu.Lock()
		_ = m[0]
		mu.Unlock()
		m[1] = 1
	}()
	mu.Lock()
	_ = m[2]
	mu.Unlock()
}

func mapPerIterationSilent() {
	for i := range 8 {
		m := map[int]int{}
		go func() {
			m[i] = i
		}()
		_ = m
	}
}

func mapPerIterationParentReported() int {
	total := 0
	for i := range 8 {
		m := map[int]int{}
		go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
			m[i] = i
		}()
		total += m[0]
	}
	return total
}

func mapLenSilent() {
	m := map[int]int{}
	go func() {
		m[0] = 1
	}()
	_ = len(m)
}

func mapNestedLitReported() {
	func() {
		m := map[int]int{}
		for i := range 8 {
			go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
				m[i] = i
			}()
		}
	}()
}

func mapCaseReadReported(k int) {
	m := map[int]int{}
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		m[1] = 1
	}()
	switch {
	case m[k] == 0:
	}
}

func mapWholeAssignSilent() {
	var m map[int]int
	for _, x := range xs() {
		go func() {
			m = map[int]int{x: x}
		}()
	}
	_ = m
}

func xs() []int { return []int{1, 2, 3} }

func printOne() {}

func mapNilLoopSwitchSilent() {
	m := map[int]int{}
	done := make(chan struct{})
	go func() {
		m[0] = 1
		close(done)
	}()
	<-done
	_ = m[0]
	for {
		break
	}
	switch {
	case false:
		_ = m
	default:
	}
	if n := len(m); n >= 0 {
		_ = n
	}
	return
}

func mapNilDeclSelectSilent() {
	m := map[int]int{}
	done := make(chan struct{})
	go func() {
		m[0] = 1
		close(done)
	}()
	<-done
	_ = m[0]
	var v int
	_ = v
	s := []int{1}
	_ = s[:]
	select {
	case <-done:
	default:
	}
	go printOne()
	defer printOne()
}

func mapGuardedReturnSilent() map[int]int {
	var mu sync.Mutex
	m := map[int]int{}
	for i := range 8 {
		go func() {
			mu.Lock()
			m[i] = i
			mu.Unlock()
		}()
	}
	return m
}

func mapClearReported() {
	m := map[int]int{1: 1}
	go func() { // want `m is a map written by this goroutine and accessed concurrently, which crashes the process: guard every access with one mutex`
		clear(m)
	}()
	_ = m[1]
}

type recPtr *recPtr

func recPtrUser(p recPtr) {}

func mapRecPtrSilent(p recPtr) {
	recPtrUser(p)
}

func mapPerIterationBeforeSpawnSilent() {
	type job struct{ Name string }
	jobs := []job{{Name: "a"}}
	work := func(j job) int { return len(j.Name) }
	for _, jb := range jobs {
		results := map[string]int{}
		results["seed"] = 0
		go func() {
			results[jb.Name] = work(jb)
		}()
		_ = results
	}
}
