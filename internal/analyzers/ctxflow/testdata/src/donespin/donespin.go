package donespin

import "context"

func use(...any) {}

type doneT struct{}

func (doneT) Done(int) chan int {
	ch := make(chan int)
	close(ch)
	return ch
}

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

func donespinSilentRange(ctx context.Context, xs []int) {
	for range xs {
		select {
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

func donespinSilentCustomDone(ctx context.Context, t doneT) {
	for {
		select {
		case <-t.Done(1):
			return
		default:
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

func donespinSilentWorkAfterFor(ctx context.Context) int {
	for {
		select {
		case <-ctx.Done():
			return -1
		default:
		}
		use(1)
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

func donespinSilentReturn(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			return
		}
	}
}

func donespinSilentGoto(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			goto done
		}
	}
done:
}

func donespinSilentLabeledBreak(ctx context.Context) {
loop:
	for {
		select {
		case <-ctx.Done():
			return
		default:
			break loop
		}
	}
}

func donespinSilentBoundedFor(ctx context.Context) {
	for i := 0; i < 3; i++ {
		select {
		case <-ctx.Done():
			return
		default:
			_ = i
		}
	}
}

func donespinSilentCondFor(ctx context.Context, cond bool) {
	for cond {
		select {
		case <-ctx.Done():
			return
		default:
			return
		}
	}
}

func donespinReportedCond(ctx context.Context, cond bool) {
	for cond {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
		}
	}
}
func donespinReportedInnerBreak(ctx context.Context, xs []int) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
		inner:
			for _, x := range xs {
				if x > 0 {
					break inner
				}
			}
		}
	}
}

func donespinReportedGotoInner(ctx context.Context) {
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			goto next
		next:
			_ = 1
		}
	}
}

func donespinSilentShadowLoop(ctx context.Context) {
	helper := func() {
	loop:
		for i := 0; i < 3; i++ {
			break loop
		}
	}
	helper()
loop:
	for {
		select {
		case <-ctx.Done():
			return
		default:
			break loop
		}
	}
}

func donespinReportedShadowSkip(ctx context.Context, n int) {
	_ = func() {
	skip:
		for range 1 {
			break skip
		}
	}
	for {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			if n > 0 {
				goto skip
			}
			n++
		skip:
			n--
		}
	}
}

func donespinSilentPostLoop(ctx context.Context, n int) {
	i := 0
	for ; i < n; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func donespinSilentContinueOuter(ctx context.Context, xs []int) int {
	n := 0
outer:
	for range xs {
		for {
			select {
			case <-ctx.Done():
				return -1
			default:
				n++
				continue outer
			}
		}
	}
	return n
}

func donespinSilentRangeDefault(ctx context.Context, ch <-chan int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			for v := range ch {
				_ = v
			}
		}
	}
}

func donespinSilentEmptySelect(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			select {}
		}
	}
}

func donespinSilentBodyAdvanced(ctx context.Context, n int) int {
	i := 0
	for i < n {
		select {
		case <-ctx.Done():
			return i
		default:
			i++
		}
	}
	return i
}

func donespinReportedConditionalAdvance(ctx context.Context, i, n int, step bool) {
	for i < n {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			if step {
				i++
			}
		}
	}
}

func donespinSilentAdvanceBothBranches(ctx context.Context, i, n int, big bool) {
	for i < n {
		select {
		case <-ctx.Done():
			return
		default:
			if big {
				i += 2
			} else {
				i++
			}
		}
	}
}

func donespinSilentAdvanceBlock(ctx context.Context, i, n int) {
	for i < n {
		select {
		case <-ctx.Done():
			return
		default:
			{
				i++
			}
		}
	}
}

func donespinReportedContinueBeforeAdvance(ctx context.Context, i, n int, a, b bool) {
	for i < n {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			if a {
				if b {
					continue
				}
				i++
			} else {
				i++
			}
		}
	}
}

func donespinReportedContinueInBlock(ctx context.Context, i, n int, b bool) {
	for i < n {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			{
				if b {
					continue
				}
				i++
			}
		}
	}
}

func donespinReportedContinueTopLevel(ctx context.Context, i, n int, b bool) {
	for i < n {
		select { // want "this loop polls the context with a default case and never blocks"
		case <-ctx.Done():
			return
		default:
			if b {
				continue
			}
			i++
		}
	}
}
