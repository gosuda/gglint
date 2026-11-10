package fanout

import "sync"

func fanoutReported() {
	var out []int
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			out = append(out, x) // want `out is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutSilent() {
	xs := []int{1, 2, 3}
	out := make([]int, len(xs))
	for i, x := range xs {
		go func() {
			out[i] = x
		}()
	}
}

var fanoutPkg int

func fanoutPkgReported() {
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			fanoutPkg = x // want `fanoutPkg is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutIncReported() {
	var n int
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			n++ // want `n is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
			_ = x
		}()
	}
	_ = n
}

func fanoutInnerSilent() {
	xs := []int{1, 2, 3}
	for _, x := range xs {
		out := 0
		go func() {
			out = x
		}()
		_ = out
	}
}

func fanoutGuardedSilent() {
	var out int
	var mu sync.Mutex
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			mu.Lock()
			out = x
			mu.Unlock()
		}()
	}
	_ = out
}
