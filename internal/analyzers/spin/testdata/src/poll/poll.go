package poll

import (
	"context"
	"sync/atomic"
	"time"
)

func use(any) {}

func pollReported(ch chan int) {
	n := 0
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			n++
		}
	}
}

func pollReportedTwoComm(ctx context.Context, ch chan int) {
	n := 0
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case <-ctx.Done():
			return
		case v := <-ch:
			use(v)
		default:
			n++
		}
	}
}

func pollSilent(ch chan int, d time.Duration) {
	n := 0
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			time.Sleep(max(d, time.Millisecond))
			n++
		}
	}
}

func pollSilentEmptyDefault(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
		}
	}
}

func pollSilentNoDefault(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		}
	}
}

func pollReportedNoComm() {
	n := 0
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		default:
			n++
		}
	}
}

func pollG() int { time.Sleep(time.Millisecond); return 1 }

func pollSilentOverwrite(ch chan int, n int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			n += pollG() + -n
		}
	}
}

func pollReportedLen(ch chan int, xs []int) {
	n := 0
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			n += len(xs)
		}
	}
}

func pollSilentNonSelect(ch chan int) {
	for {
		use(ch)
	}
}

func pollSilentDefaultSend(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			ch <- 1
		}
	}
}

func pollSilentDefaultRecv(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			_ = <-ch
		}
	}
}

func pollSilentDefaultSelect(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			select {
			case w := <-ch:
				use(w)
			}
		}
	}
}

func pollSilentDone(ctx context.Context, ch chan int) {
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
			n++
		}
	}
}

func pollSilentDefaultReturn(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			return
		}
	}
}

func pollSilentDefaultRange(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			for v := range ch {
				use(v)
			}
		}
	}
}

func pollSilentBounded(ch chan int) {
	n := 0
	for i := 0; i < 3; i++ {
		select {
		case v := <-ch:
			use(v)
		default:
			n++
		}
	}
	use(n)
}

func pollSilentDefaultGoto(ch chan int) {
	n := 0
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			n++
			goto Done
		}
	}
Done:
	use(n)
}

func pollSilentDefaultBreakOuter(ch chan int) {
	n := 0
Outer:
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			n++
			break Outer
		}
	}
	use(n)
}

func pollSilentContinueOuter(ch chan int, start chan struct{}) {
	n := 0
Outer:
	for range start {
		for {
			select {
			case v := <-ch:
				use(v)
			default:
				n++
				continue Outer
			}
		}
	}
	use(n)
}

func pollSilentDefaultSwitch(ch chan int, n int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			switch n {
			case 0:
				return
			default:
				return
			}
		}
	}
}

func pollSilentDefaultTypeSwitch(ch chan int, v any) {
	for {
		select {
		case w := <-ch:
			use(w)
		default:
			switch v.(type) {
			case int:
				return
			default:
				return
			}
		}
	}
}

func pollSilentDefaultLabeled(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			goto Done
		Done:
			return
		}
	}
}

func pollReportedSwitchBreak(ch <-chan int, n int, quiet bool) {
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			switch n {
			case 0:
				if quiet {
					break
				}
				return
			default:
				return
			}
		}
	}
}

func pollReportedLabeledSwitch(ch <-chan int, n int) {
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
		sw:
			switch n {
			case 0:
				break sw
			default:
				break sw
			}
		}
	}
}

func pollReportedDefaultSelectBreak(ch chan int) {
	n := 0
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			n++
			break
		}
	}
	use(n)
}

func pollSilentDefaultBlock(ch chan int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			{
				return
			}
		}
	}
}

func pollSilentDefaultIfElse(ch chan int, n int) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			if n > 0 {
				return
			} else {
				return
			}
		}
	}
}

func pollReportedInitPost(ch chan int) int {
	n := 0
	for i := 0; ; i++ { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			return v
		default:
			n += i
		}
	}
}
func pollReportedCond(ch chan int, stop *atomic.Bool) {
	n := 0
	for !stop.Load() { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			n++
		}
	}
	use(n)
}
func pollReportedCountedFlag(ch chan int, stop *atomic.Bool) {
	n := 0
	for i := 0; !stop.Load(); i++ { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			n++
		}
	}
	use(n)
}
func pollReportedCountedWrite(ch chan int, n int) {
	for i := 0; i < n; i++ { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			i--
		}
	}
}
func pollSilentNestedWait(ch chan int, stop chan struct{}, d time.Duration) {
	for {
		select {
		case v := <-ch:
			use(v)
		default:
			select {
			case <-stop:
				use(0)
			default:
				time.Sleep(d)
			}
		}
	}
}

func pollReportedNestedEmptyDefault(ch chan int) {
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		default:
			select {
			case w := <-ch:
				use(w)
			default:
			}
		}
	}
}
