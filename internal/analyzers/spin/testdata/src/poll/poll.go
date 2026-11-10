package poll

import (
	"context"
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

func pollReportedTwoComm(ch, ch2 chan int) {
	n := 0
	for { // want `no case blocks and the default case does not wait, so this loop spins: remove the default case, or wait in it`
		select {
		case v := <-ch:
			use(v)
		case w := <-ch2:
			use(w)
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
			time.Sleep(d)
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

func pollG() int { return 1 }

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

func pollSilentMultiStmt(ch chan int) {
	for {
		use(ch)
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
			default:
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
