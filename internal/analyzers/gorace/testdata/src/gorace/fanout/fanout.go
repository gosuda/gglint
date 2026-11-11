package fanout

import (
	"flag"
	"sync"
	"sync/atomic"
)

func fanoutReported() {
	var out []int
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			out = append(out, x) // want `out is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
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
			fanoutPkg = x // want `fanoutPkg is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutIncReported() {
	var n int
	xs := []int{1, 2, 3}
	for _, x := range xs {
		go func() {
			n++ // want `n is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
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
			_ = out
		}()
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
			_ = out
			mu.Unlock()
		}()
	}
}

func fanoutNestedReported(rows [][]int) {
	for _, row := range rows {
		var sum int
		for _, x := range row {
			go func() {
				sum += x // want `sum is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
			}()
		}
		_ = sum
	}
}

func fanoutParamReported(n int, xs []int) {
	for _, x := range xs {
		go func() {
			n += x // want `n is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutMapVarReported(xs []int) {
	var m map[int]int
	for _, x := range xs {
		go func() {
			m = map[int]int{x: x} // want `m is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
		}()
	}
	_ = m
}

func fanoutRangeAssignReported(xs []int) {
	var i int
	for range xs {
		go func() {
			for i = range xs { // want `i is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
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
			flag.Usage = nil // want `flag.Usage is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
		}()
	}
}

func fanoutNestedSpawnReported(xs []int) {
	var out int
	go func() {
		for _, x := range xs {
			go func() {
				out = x // want `out is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
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

func fanoutSingleSilent() int {
	out := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		out = 1
	}()
	wg.Wait()
	return out
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
			_ = id
			c.L.Unlock()
			_ = x
		}()
	}
}

func fanoutDeleteClearSilent() {
	m := map[int]int{1: 1}
	var mu sync.Mutex
	for i := range 8 {
		go func() {
			mu.Lock()
			delete(m, i)
			mu.Unlock()
		}()
	}
	for range 8 {
		go func() {
			mu.Lock()
			clear(m)
			mu.Unlock()
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
			_ = v
			fanoutRangeMu.Unlock()
		}()
	}
}

func fanoutSwitchBreakReported(xs []int) {
	var out int
	for _, x := range xs {
		switch {
		case x > 0:
			go func() {
				out = x // want `out is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
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
				out = x // want `out is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
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
			for _, v = range xs { // want `v is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
				fanoutHeaderMu.Unlock()
				if v > 0 {
					return
				}
			}
		}()
	}
	_ = v
}

func fanoutBreakIfSilent(xs []int) {
	var out int
	for _, x := range xs {
		if x > 0 {
			go func() {
				out = x
				_ = out
			}()
			break
		}
	}
}

var fanoutInnerMu sync.Mutex

func fanoutInnerLoopGuardedSilent(xs []int) {
	var v int
	for range 2 {
		go func() {
			fanoutInnerMu.Lock()
			for _, v = range xs {
				for i := 0; i < 2; i++ {
					if i == 0 {
						fanoutInnerMu.Unlock()
					}
				}
				fanoutInnerMu.Lock()
			}
			_ = v
			fanoutInnerMu.Unlock()
		}()
	}
}

func fanoutTokenRingSilent(n int) int {
	if n < 1 {
		return 0
	}
	ch := make([]chan bool, n)
	for g := range ch {
		ch[g] = make(chan bool)
	}
	done := make(chan bool)
	count := 0
	for g := range n {
		go func() {
			<-ch[g]
			count++
			if g+1 < n {
				ch[g+1] <- true
			} else {
				done <- true
			}
		}()
	}
	ch[0] <- true
	<-done
	return count
}

func fanoutAtomicHandoffSilent() int {
	var turn atomic.Int32
	x := 0
	for g := range 2 {
		go func() {
			for turn.Load() != int32(g) {
			}
			x = g
			turn.Store(int32(g + 1))
		}()
	}
	for turn.Load() != 2 {
	}
	return x
}

func fanoutWgGoBreakSilent(xs []int, wg *sync.WaitGroup) int {
	var out int
	for _, x := range xs {
		wg.Go(func() {
			out = x
		})
		break
	}
	wg.Wait()
	return out
}

func fanoutStartGateReported(xs []int, start chan struct{}) int {
	total := 0
	for _, x := range xs {
		go func() {
			<-start
			total += x // want `total is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
		}()
	}
	close(start)
	return total
}

func fanoutReleasedWriteReported(xs []int) int {
	var mu sync.Mutex
	out := 0
	mu.Lock()
	for _, x := range xs {
		go func() {
			mu.Lock()
			mu.Unlock()
			out = x // want `out is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
		}()
	}
	mu.Unlock()
	return out
}

func fanoutTokenDirectSilent(xs []int) int {
	in := make(chan int)
	out := make(chan int)
	total := 0
	for _, x := range xs {
		go func() {
			v := <-in
			total += x + v
			out <- total
		}()
	}
	for _, x := range xs {
		in <- x
		<-out
	}
	return total
}

func fanoutSemaphoreSilent(xs []int) int {
	sem := make(chan struct{}, 1)
	done := make(chan struct{})
	out := 0
	for _, x := range xs {
		go func() {
			sem <- struct{}{}
			out = x
			<-sem
			done <- struct{}{}
		}()
	}
	for range xs {
		<-done
	}
	return out
}

func fanoutTokenRecvSendSilent(xs []int) int {
	tok := make(chan struct{}, 1)
	tok <- struct{}{}
	total := 0
	for _, x := range xs {
		go func() {
			<-tok
			total += x
			tok <- struct{}{}
		}()
	}
	return 0
}

func fanoutCondSemReported(xs []int) int {
	sem := make(chan struct{}, 1)
	out := 0
	for _, x := range xs {
		go func() {
			if x > 0 {
				sem <- struct{}{}
			}
			out = x // want `out is written by every goroutine this loop starts, so the writes race when two of them overlap: collect results through a channel, or guard the write with a mutex`
			if x > 0 {
				<-sem
			}
		}()
	}
	return out
}

func fanoutParamSemSilent(sem chan struct{}, xs []int) int {
	total := 0
	var wg sync.WaitGroup
	for _, x := range xs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			total += x
			<-sem
		}()
	}
	wg.Wait()
	return total
}
