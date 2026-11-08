package standard

import (
	"context"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func work() {}

func namedFunction() {
	go work() // want "goroutine is not joined: use errgroup.Group.Go or sync.WaitGroup.Go"
}

func literalWithoutEvidence() {
	go func() { // want "goroutine is not joined: use errgroup.Group.Go or sync.WaitGroup.Go"
		work()
	}()
}

func unjoinedLoop() {
	go func() { // want "goroutine is not joined: use errgroup.Group.Go or sync.WaitGroup.Go"
		for { // want `goroutine loop has no exit path: return when ctx\.Done\(\) is closed`
			work()
		}
	}()
}

func waitGroupDone() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		work()
	}()
	wg.Wait()
}

func waitGroupPointerDone(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		work()
	}()
}

func doneWithoutDefer(wg *sync.WaitGroup) {
	go func() { // want "goroutine is not joined"
		wg.Done()
	}()
}

type holder struct {
	sync.WaitGroup
}

func embeddedWaitGroupDone(h *holder) {
	go func() { // want "goroutine is not joined"
		defer h.Done()
	}()
}

func doneInNestedLiteral(wg *sync.WaitGroup) {
	go func() { // want "goroutine is not joined"
		func() {
			defer wg.Done()
		}()
	}()
}

func closesLocal() <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		ch <- 1
	}()
	return ch
}

func deferredCloseOnly() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	return done
}

func closeWithoutDefer() {
	done := make(chan struct{})
	go func() { // want "goroutine is not joined"
		work()
		close(done)
	}()
	<-done
}

func sendsLocal() int {
	result := make(chan int, 1)
	go func() {
		result <- 1
	}()
	return <-result
}

func sendsInSelect(ctx context.Context) {
	result := make(chan int)
	go func() {
		select {
		case result <- 1:
		case <-ctx.Done():
		}
	}()
	<-result
}

func sendsParameter(out chan<- int) {
	go func() {
		out <- 1
	}()
}

func sendsLiteralLocal() {
	go func() { // want "goroutine is not joined"
		ch := make(chan int, 1)
		ch <- 1
	}()
}

var global = make(chan int, 1)

func sendsGlobal() {
	go func() { // want "goroutine is not joined"
		global <- 1
	}()
}

func loopWithoutExit(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for { // want `goroutine loop has no exit path: return when ctx\.Done\(\) is closed`
			work()
		}
	}()
}

func loopWithPostWithoutExit(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for i := 0; ; i++ { // want "goroutine loop has no exit path"
			_ = i
		}
	}()
}

func loopWithCondition(ctx context.Context, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			work()
		}
	}()
}

func loopReturnsOnDone(ctx context.Context, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				work()
			}
		}
	}()
}

func breakLeavesSelectOnly(ctx context.Context, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for { // want "goroutine loop has no exit path"
			select {
			case <-ctx.Done():
				break
			default:
				work()
			}
		}
	}()
}

func breakLeavesSwitchOnly(n int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for { // want "goroutine loop has no exit path"
			switch n {
			case 0:
				break
			}
		}
	}()
}

func breakLeavesInnerLoopOnly(n int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for { // want "goroutine loop has no exit path"
			for range n {
				break
			}
		}
	}()
}

func labeledBreakLeavesLoop(ctx context.Context, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
	loop:
		for {
			select {
			case <-ctx.Done():
				break loop
			default:
				work()
			}
		}
	}()
}

func directBreak(ctx context.Context, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				break
			}
			work()
		}
	}()
}

func labeledContinueOwnLoop(n int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
	loop:
		for { // want "goroutine loop has no exit path"
			if n > 0 {
				continue loop
			}
		}
	}()
}

func labeledContinueOuterLoop(items []int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
	outer:
		for _, v := range items {
			for {
				if v > 0 {
					continue outer
				}
			}
		}
	}()
}

func gotoLeavesLoop(n int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for {
			if n > 0 {
				goto done
			}
		}
	done:
		work()
	}()
}

func terminatingCalls(n int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for {
			if n > 0 {
				panic("stop")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			os.Exit(1)
		}
	}()
	go func() {
		defer wg.Done()
		for {
			runtime.Goexit()
		}
	}()
	go func() {
		defer wg.Done()
		for {
			log.Fatalf("stop %d", n)
		}
	}()
}

func returnInNestedLiteral(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for { // want "goroutine loop has no exit path"
			func() {
				return
			}()
		}
	}()
}

func loopInNestedLiteral(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		spin := func() {
			for {
				work()
			}
		}
		_ = spin
	}()
}

func tickLoop(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for range time.Tick(time.Second) { // want "goroutine loop has no exit path"
			work()
		}
	}()
}

func tickerLoop(wg *sync.WaitGroup) {
	ticker := time.NewTicker(time.Second)
	go func() {
		defer wg.Done()
		for range ticker.C { // want "goroutine loop has no exit path"
			work()
		}
	}()
}

func tickerLoopReturns(ctx context.Context, wg *sync.WaitGroup) {
	ticker := time.NewTicker(time.Second)
	go func() {
		defer wg.Done()
		defer ticker.Stop()
		for range ticker.C {
			if ctx.Err() != nil {
				return
			}
		}
	}()
}

func rangeOverChannel(in <-chan int, wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		for v := range in {
			_ = v
		}
	}()
}

func emptySelect(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		select {} // want "goroutine blocks forever on empty select"
	}()
}

func emptySelectOutsideGoroutine() {
	select {}
}

func loopOutsideGoroutine() {
	for {
		work()
	}
}

func waitGroupGoBody() {
	var wg sync.WaitGroup
	wg.Go(func() {
		for { // want "goroutine loop has no exit path"
			work()
		}
	})
	wg.Wait()
}

func errgroupBody() error {
	var g errgroup.Group
	g.Go(func() error {
		select {} // want "goroutine blocks forever on empty select"
	})
	g.TryGo(func() error {
		for range time.Tick(time.Second) { // want "goroutine loop has no exit path"
			work()
		}
		return nil
	})
	return g.Wait()
}

func fanOutAllowedInStandard(items []int) error {
	var wg sync.WaitGroup
	var g errgroup.Group
	for range items {
		wg.Go(work)
		g.Go(func() error { return nil })
	}
	wg.Wait()
	return g.Wait()
}
