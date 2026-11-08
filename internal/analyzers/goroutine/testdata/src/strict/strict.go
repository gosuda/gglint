package strict

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"
)

func work() {}

func bareNamedFunction() {
	go work() // want "bare go statement: use errgroup.Group.Go or sync.WaitGroup.Go"
}

func bareJoinedLiteral() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // want "bare go statement: use errgroup.Group.Go or sync.WaitGroup.Go"
		defer wg.Done()
		work()
	}()
	wg.Wait()
}

func bareInLoop(items []int) {
	for range items {
		go work() // want "bare go statement"
	}
}

func bareWithEmptySelect() {
	go func() { // want "bare go statement"
		select {} // want "goroutine blocks forever on empty select"
	}()
}

func waitGroupGo() {
	var wg sync.WaitGroup
	wg.Go(work)
	wg.Wait()
}

func errgroupGo() error {
	var g errgroup.Group
	g.Go(func() error { return nil })
	g.TryGo(func() error { return nil })
	return g.Wait()
}

func waitGroupGoLoop(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() {
		for { // want `goroutine loop has no exit path: return when ctx\.Done\(\) is closed`
			if ctx.Err() != nil {
				continue
			}
		}
	})
	wg.Wait()
}

func waitGroupFanOut(items []int) {
	var wg sync.WaitGroup
	for range items {
		wg.Go(work) // want "unbounded goroutine fan-out in loop: call SetLimit on the errgroup.Group"
	}
	wg.Wait()
}

func errgroupFanOut(items []int) error {
	var g errgroup.Group
	for range items {
		g.Go(func() error { return nil }) // want "unbounded goroutine fan-out in loop: call SetLimit on the errgroup.Group"
	}
	return g.Wait()
}

func tryGoFanOut(ctx context.Context) error {
	g := new(errgroup.Group)
	for ctx.Err() == nil {
		if !g.TryGo(func() error { return nil }) { // want "unbounded goroutine fan-out in loop"
			break
		}
	}
	return g.Wait()
}

func limitedFanOut(items []int) error {
	var g errgroup.Group
	g.SetLimit(4)
	for range items {
		g.Go(func() error { return nil })
	}
	return g.Wait()
}

func limitedThroughPointer(items []int) error {
	g := &errgroup.Group{}
	g.SetLimit(4)
	for range items {
		g.TryGo(func() error { return nil })
	}
	return g.Wait()
}

type pool struct {
	group errgroup.Group
}

func (p *pool) limitedField(items []int) error {
	p.group.SetLimit(2)
	for range items {
		p.group.Go(func() error { return nil })
	}
	return p.group.Wait()
}

func limitedAfterLoop(items []int) error {
	var g errgroup.Group
	for range items {
		g.Go(func() error { return nil }) // want "unbounded goroutine fan-out in loop"
	}
	g.SetLimit(4)
	return g.Wait()
}

func limitedOtherGroup(items []int) error {
	var g, h errgroup.Group
	h.SetLimit(4)
	for range items {
		g.Go(func() error { return nil }) // want "unbounded goroutine fan-out in loop"
	}
	if err := h.Wait(); err != nil {
		return err
	}
	return g.Wait()
}

func spawnOutsideLoop(items []int) error {
	var g errgroup.Group
	g.Go(func() error {
		for range items {
			work()
		}
		return nil
	})
	return g.Wait()
}

func loopInSeparateLiteral(items []int) {
	var wg sync.WaitGroup
	for range items {
		spawn := func() {
			wg.Go(work)
		}
		spawn()
	}
	wg.Wait()
}
