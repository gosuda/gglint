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
