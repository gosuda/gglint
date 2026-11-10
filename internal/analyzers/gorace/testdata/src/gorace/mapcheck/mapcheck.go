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
	go func() {
		mu.Lock()
		m[2] = 2
		mu.Unlock()
	}()
	_ = mu
}

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
