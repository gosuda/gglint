package cancelreturned

import (
	"context"
	"time"
)

func useAny(...any) {}

func otherCall() (context.Context, context.CancelFunc) { return nil, nil }

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

func cancelreturnedSilentEllipsis(ctx context.Context, xs []any) context.Context {
	c, cancel := context.WithTimeout(ctx, 0)
	defer useAny(xs...)
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
