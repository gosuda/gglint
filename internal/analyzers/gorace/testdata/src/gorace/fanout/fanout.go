package fanout

import (
	"flag"
	"sync"
)

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

func fanoutNestedReported(rows [][]int) {
	for _, row := range rows {
		var sum int
		for _, x := range row {
			go func() {
				sum += x // want `sum is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
			}()
		}
		_ = sum
	}
}

func fanoutParamReported(n int, xs []int) {
	for _, x := range xs {
		go func() {
			n += x // want `n is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutMapVarReported(xs []int) {
	var m map[int]int
	for _, x := range xs {
		go func() {
			m = map[int]int{x: x} // want `m is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
		}()
	}
	_ = m
}

func fanoutRangeAssignReported(xs []int) {
	var i int
	for range xs {
		go func() {
			for i = range xs { // want `i is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
			}
		}()
		_ = i
	}
}

func fanoutLoopVarSilent(xs []int) {
	for i := range xs {
		_ = i
		go func() {
			i = 0
		}()
	}
}

func fanoutFlagReported(xs []int) {
	for range xs {
		go func() {
			flag.Usage = nil // want `flag.Usage is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutNestedSpawnReported(xs []int) {
	var out int
	go func() {
		for _, x := range xs {
			go func() {
				out = x // want `out is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
			}()
		}
	}()
	_ = out
}

func fanoutOnceSilent(n *int, xs []int) {
	for _, x := range xs {
		go func() {
			*n = x
		}()
		break
	}
}

type fanoutLockerGroup struct{ L sync.Locker }

func fanoutLockerSilent() {
	id := 0
	c := fanoutLockerGroup{L: &sync.Mutex{}}
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			c.L.Lock()
			id++
			c.L.Unlock()
			_ = x
		}()
	}
	_ = id
}

func fanoutDeleteClearSilent() {
	m := map[int]int{1: 1}
	for i := range 8 {
		go func() {
			delete(m, i)
		}()
	}
	for range 8 {
		go func() {
			clear(m)
		}()
	}
	_ = m
}

var fanoutRangeMu sync.Mutex

func fanoutRangeGuardedSilent(xs []int) {
	var v int
	for range 3 {
		go func() {
			fanoutRangeMu.Lock()
			for _, v = range xs {
			}
			fanoutRangeMu.Unlock()
		}()
	}
	_ = v
}

func fanoutSwitchBreakReported(xs []int) {
	var out int
	for _, x := range xs {
		switch {
		case x > 0:
			go func() {
				out = x // want `out is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
			}()
			break
		}
	}
	_ = out
}

func fanoutInnerBreakReported(xs []int) {
	var out int
	for _, x := range xs {
		for range 2 {
			go func() {
				out = x // want `out is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
			}()
			break
		}
	}
	_ = out
}

var fanoutHeaderMu sync.Mutex

func fanoutHeaderUnlockedReported(xs []int) {
	var v int
	for range 2 {
		go func() {
			fanoutHeaderMu.Lock()
			for _, v = range xs { // want `v is written by every goroutine this loop starts, so the writes race: collect results through a channel, or guard the write with a mutex`
				fanoutHeaderMu.Unlock()
				if v > 0 {
					return
				}
			}
		}()
	}
	_ = v
}
