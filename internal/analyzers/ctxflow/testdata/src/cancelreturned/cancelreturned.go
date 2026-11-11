package cancelreturned

import (
	"context"
	"time"
)

func useAny(...any) {}

func otherCall() (context.Context, context.CancelFunc) { return context.Background(), func() {} }

type holder struct {
	c context.Context
}

func cancelreturnedReported(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func cancelreturnedReportedCause(ctx context.Context) context.Context {
	c, cancel := context.WithCancelCause(ctx)
	defer cancel(nil) // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func cancelreturnedSilent(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel()
	return c, cancel
}

func cancelreturnedReportedVar(ctx context.Context) context.Context {
	var c, cancel = context.WithTimeout(ctx, 0)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func cancelreturnedSilentVar(ctx context.Context) (context.Context, context.CancelFunc) {
	var c, cancel = context.WithTimeout(ctx, 0)
	defer cancel()
	return c, cancel
}

func cancelreturnedSilentNoDefer(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	_ = cancel
	return c
}

func cancelreturnedSilentNoCtx(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel()
	_ = c
	return nil
}

func cancelreturnedSilentOtherCall(ctx context.Context) context.Context {
	x := 1
	_ = x
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	_ = cancel
	_ = c
	return ctx
}

func cancelreturnedSilentNonWith(ctx context.Context) context.Context {
	c, cancel := otherCall()
	defer cancel()
	_ = c
	return ctx
}

func cancelreturnedSilentField(ctx context.Context, h *holder) context.Context {
	h.c, _ = context.WithTimeout(ctx, 0)
	return ctx
}

func cancelreturnedSilentDeferLit(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	defer func() {}()
	_ = cancel
	_ = c
	return ctx
}

func cancelreturnedSilentNestedDefer(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	func() {
		defer cancel()
	}()
	_ = c
	return ctx
}

func cancelreturnedReportedNested(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	func() context.Context {
		return c
	}()
	return c
}

func cancelreturnedReportedNestedAssign(ctx context.Context) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	func() {
		x, y := context.WithTimeout(ctx, 0)
		_ = x
		_ = y
	}()
	return c
}

func cancelreturnedSilentConst(ctx context.Context) context.Context {
	const k = 1
	_ = k
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel()
	_ = cancel
	_ = c
	return ctx
}

func cancelreturnedSilentVarSingle(ctx context.Context) context.Context {
	var x = 1
	_ = x
	c, cancel := context.WithTimeout(ctx, 0)
	defer cancel()
	_ = cancel
	_ = c
	return ctx
}

func cancelreturnedSilentVarOther(ctx context.Context) context.Context {
	var a, b = otherCall()
	_ = a
	_ = b
	return ctx
}

func cancelreturnedSilentVarBlank(ctx context.Context) context.Context {
	var _, _ = context.WithTimeout(ctx, 0)
	return ctx
}
func cancelreturnedReportedDeadline(ctx context.Context) context.Context {
	c, cancel := context.WithDeadline(ctx, time.Now())
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func cancelreturnedReportedTimeoutCause(ctx context.Context) context.Context {
	c, cancel := context.WithTimeoutCause(ctx, 0, nil)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func cancelreturnedReportedDeadlineCause(ctx context.Context) context.Context {
	c, cancel := context.WithDeadlineCause(ctx, time.Now(), nil)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func cancelreturnedReportedCancel(ctx context.Context) context.Context {
	c, cancel := context.WithCancel(ctx)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return c
}

func workOf(ctx context.Context) error { return nil }

func cancelreturnedSilentCallResult(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	return workOf(ctx)
}

func cancelreturnedSilentMethodResult(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	return ctx.Err()
}

func detachCtx(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

func cancelreturnedReportedComposite(parent context.Context) *holder {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return &holder{c: c}
}

func cancelreturnedSilentCompositeCall(parent context.Context) *holder {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	_ = c
	return &holder{c: detachCtx(c)}
}

type reproKey struct{}

func cancelreturnedReportedDerivedCall(parent context.Context) context.Context {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return context.WithValue(c, reproKey{}, 1)
}

func cancelreturnedReportedAddr(parent context.Context) *context.Context {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return &c
}

func cancelreturnedSilentWithoutCancel(parent context.Context) context.Context {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	_ = c
	return context.WithoutCancel(c)
}

func cancelreturnedReportedNestedValue(parent context.Context) *holder {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return &holder{c: context.WithValue(c, reproKey{}, 1)}
}

func cancelreturnedSilentReassigned(parent context.Context) (context.Context, error) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	ctx = parent
	return ctx, nil
}

func cancelreturnedReportedDerivedTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return context.WithTimeout(c, time.Second)
}

func cancelreturnedSilentNestedReturn() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := func() context.Context { return ctx }
	useAny(f)
}

func fetchForRedeclare(ctx context.Context) (context.Context, error) { return ctx, nil }

func cancelreturnedSilentRedeclared(parent context.Context) (context.Context, error) {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	c, err := fetchForRedeclare(parent)
	return c, err
}

func cancelreturnedSilentLiteralAssign(parent context.Context) context.Context {
	c, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	reset := func() { c = context.Background() }
	reset()
	return c
}

func cancelreturnedReportedBareReturn(parent context.Context) (ctx context.Context, err error) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel() // want "the deferred cancel runs before the caller uses the returned context"
	return
}
