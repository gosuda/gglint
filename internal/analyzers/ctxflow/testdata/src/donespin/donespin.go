package donespin

import "context"

func use(...any) {}

type doneT struct{}

func (doneT) Done(int) chan int { return nil }

func donespinReported(ctx context.Context) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
		}
	}
}

func donespinSilent(ctx context.Context, ch chan int) {
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-ch:
			use(v)
		}
	}
}

func donespinSilentCall(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			use(1)
		}
	}
}

func donespinReportedAssign(ctx context.Context) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case _ = <-ctx.Done():
			return
		default:
		}
	}
}

func donespinReportedRange(ctx context.Context, xs []int) {
	for range xs {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
		}
	}
}

func donespinSilentNoFor(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
	}
}

func donespinSilentThree(ctx context.Context, ch chan int) {
	for {
		select {
		case <-ctx.Done():
			return
		case v := <-ch:
			use(v)
		default:
		}
	}
}

func donespinSilentSingle(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		}
	}
}

func donespinSilentOther(ctx context.Context, ch chan int) {
	for {
		select {
		case <-ch:
			return
		default:
		}
	}
}

func donespinSilentAssignOther(ctx context.Context, ch chan int) {
	for {
		select {
		case x := <-ch:
			use(x)
		default:
		}
	}
}

func donespinSilentCustomDone(ctx context.Context, t doneT, ch chan int) {
	for {
		select {
		case <-t.Done(1):
			return
		case v := <-ch:
			use(v)
		}
	}
}

type ctxAlias = context.Context

func donespinReportedAlias(ctx ctxAlias) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
		}
	}
}

func donespinReportedConversion(ctx context.Context, n int) int64 {
	var x int64
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return x
		default:
			x = int64(n)
		}
	}
}

func donespinReportedLen(ctx context.Context, buf []byte) int {
	var n int
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return n
		default:
			n = len(buf)
		}
	}
}

func donespinSilentWorkAfter(ctx context.Context, xs []int) int {
	for _, x := range xs {
		select {
		case <-ctx.Done():
			return -1
		default:
		}
		use(x)
	}
	return 0
}

func donespinSilentWorkAfterFor(ctx context.Context) int {
	for {
		select {
		case <-ctx.Done():
			return -1
		default:
		}
		if use(1); true {
			return 0
		}
	}
}

func donespinSilentChanRecv(ctx context.Context, sem <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			<-sem
		}
	}
}

func donespinSilentChanSend(ctx context.Context, out chan int, v int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			out <- v
		}
	}
}
func donespinReportedFuncLit(ctx context.Context, ch chan int) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			f := func() { <-ch }
			_ = f
		}
	}
}

func donespinReportedNestedDefault(ctx context.Context, ch chan int) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			select {
			case v := <-ch:
				_ = v
			default:
			}
		}
	}
}

func donespinSilentNestedBlocking(ctx context.Context, ch chan int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			select {
			case v := <-ch:
				use(v)
			}
		}
	}
}
