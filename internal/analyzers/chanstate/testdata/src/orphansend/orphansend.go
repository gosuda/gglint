package orphansend

import "context"

func workOrphan() int { return 0 }

func workCall() {}

func orphansendReported(ctx context.Context) (int, error) {
	ch := make(chan int)
	go func() { ch <- workOrphan() }() // want `when the parent returns through its other select case, this send on ch blocks forever and the goroutine leaks: give ch capacity 1`
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func orphansendSilent(ctx context.Context) (int, error) {
	ch := make(chan int, 1)
	go func() { ch <- workOrphan() }()
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func orphansendSilentTwo() int {
	ch := make(chan int)
	go func() { ch <- workOrphan() }()
	_ = <-ch
	select {
	case v := <-ch:
		return v
	default:
		return -1
	}
}

func orphansendSilentNoReturn() int {
	ch := make(chan int)
	go func() { ch <- workOrphan() }()
	v := 0
	select {
	case x := <-ch:
		v = x
	default:
		v = -1
	}
	return v
}

func orphansendSilentPlainRecv() int {
	ch := make(chan int)
	go func() { ch <- workOrphan() }()
	return <-ch
}

func orphansendSilentCall() {
	ch := make(chan int)
	go workCall()
	_ = ch
}

func stepOrphan() {}

func orphansendSilentLoopDefault() {
	ch := make(chan int)
	go func() { ch <- workOrphan() }()
	for {
		select {
		case v := <-ch:
			_ = v
			return
		default:
			stepOrphan()
		}
	}
}

func orphansendReportedTwoSends(ctx context.Context) (int, error) {
	ch := make(chan int)
	logc := make(chan string, 1)
	go func() {
		logc <- "start"
		ch <- workOrphan() // want `when the parent returns through its other select case, this send on ch blocks forever and the goroutine leaks: give ch capacity 1`
	}()
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func orphansendReportedSameChan(ctx context.Context) (int, error) {
	ch := make(chan int)
	go func() {
		ch <- 1 // want `when the parent returns through its other select case, this send on ch blocks forever and the goroutine leaks: give ch capacity 1`
		ch <- 2 // want `when the parent returns through its other select case, this send on ch blocks forever and the goroutine leaks: give ch capacity 1`
	}()
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func orphansendSilentOtherReceiver(ctx context.Context) (int, error) {
	ch := make(chan int)
	go func() { ch <- workOrphan() }()
	go func() { <-ch }()
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func drainOrphan(ch chan int, n int) {
	for i := 0; i < n; i++ {
		<-ch
	}
}

func orphansendSilentCallDrain(ctx context.Context) (int, error) {
	ch := make(chan int)
	go func() { ch <- workOrphan() }()
	go drainOrphan(ch, 3)
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
