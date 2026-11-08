// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/unsupervised_goroutine.lgo.

package p

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"
)

func work() {}

func workErr() error { return nil }

func bare(ctx context.Context) error {
	go work() // want "goroutine is not joined"
	return nil
}

func bareLiteral(ctx context.Context) error {
	go func() { // want "goroutine is not joined"
		work()
	}()
	return nil
}

func supervised(ctx context.Context) error {
	var g errgroup.Group
	g.Go(func() error {
		work()
		return nil
	})
	return g.Wait()
}

func supervisedCall(ctx context.Context) error {
	var g errgroup.Group
	g.Go(workErr)
	return g.Wait()
}

func supervisedWaitGroup(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(work)
	wg.Wait()
	return nil
}
