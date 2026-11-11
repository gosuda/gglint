package root

import (
	"context"
	"net/http"
	"time"
)

func use(...any) {}

func zero() {}

func rootReported(ctx context.Context) {
	use(context.Background()) // want "a context is in scope, and this root context drops its cancellation and deadline"
	use(context.TODO())       // want "a context is in scope, and this root context drops its cancellation and deadline"
}

func rootSilent(ctx context.Context) {
	use(context.WithoutCancel(ctx))
	if ctx == nil {
		ctx = context.Background()
	}
	_ = context.Background()
	context.Background()
	use(ctx)
}

func rootSilentNoScope() {
	use(context.Background())
}

func main() {
	use(context.Background())
	f := func(ctx context.Context) { use(context.Background()) } // want "a context is in scope, and this root context drops its cancellation and deadline"
	f(nil)
}

func init() {
	use(context.Background())
	g := func(ctx context.Context) { use(context.TODO()) } // want "a context is in scope, and this root context drops its cancellation and deadline"
	g(nil)
}

func rootReportedVar(ctx context.Context) {
	var x = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	use(x)
}

func rootSilentBlankVar(ctx context.Context) {
	var _ = context.Background()
	use(ctx)
}

func rootSilentMethod(ctx context.Context) {
	use(ctx.Done())
	use(ctx.Value(0))
}

func rootSilentFuncValue(ctx context.Context) {
	f := context.WithoutCancel
	use(f(ctx))
	use(ctx)
}

func rootSilentZero(ctx context.Context) {
	zero()
	use(ctx)
}

func rootReportedAssignOther(ctx context.Context) {
	x := context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	use(x)
}

func rootReportedAssign(ctx context.Context) {
	ctx = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	use(ctx)
}

func rootReportedNotEqual(ctx context.Context) {
	if ctx != nil {
		ctx = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	}
	use(ctx)
}

func rootReportedBeforeIf(ctx context.Context) {
	ctx = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	if ctx == nil {
		use(ctx)
	}
}

func rootSilentReversed(ctx context.Context) {
	if nil == ctx {
		ctx = context.Background()
	}
	use(ctx)
}

type ctxAlias = context.Context

func rootReportedAlias(ctx ctxAlias) {
	use(context.Background()) // want "a context is in scope, and this root context drops its cancellation and deadline"
}

func rootSilentGoDefer(ctx context.Context) {
	defer context.Background()
	go context.Background()
	use(ctx)
}

func rootSilentNestedArg(ctx context.Context, k, v any) context.Context {
	if ctx == nil {
		ctx = context.WithValue(context.Background(), k, v)
	}
	return ctx
}

func rootReportedRequest(r *http.Request) {
	use(r, context.Background()) // want "a context is in scope, and this root context drops its cancellation and deadline"
}

func rootSilentCompared(ctx context.Context) {
	if ctx != context.Background() {
		use(ctx)
	}
}

type rootShared struct {
	context.Context
	values context.Context
}

func (s *rootShared) Deadline() (time.Time, bool) { return s.values.Deadline() }
func (s *rootShared) Done() <-chan struct{}       { return s.values.Done() }
func (s *rootShared) Err() error                  { return s.values.Err() }
func (s *rootShared) Value(k any) any             { return s.values.Value(k) }

func rootSilentSharedComposite(ctx context.Context) context.Context {
	return &rootShared{Context: context.Background(), values: ctx}
}

func rootReportedOrGuard(ctx context.Context, debug bool) {
	if ctx == nil || debug {
		ctx = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	}
	use(ctx)
}

func rootSilentElseGuard(ctx context.Context) {
	if ctx != nil {
		use(ctx)
	} else {
		ctx = context.Background()
	}
	use(ctx)
}

func rootSilentBlankParam(_ context.Context) {
	use(context.Background())
}

func rootSilentParens(ctx context.Context) bool {
	_ = (context.Background())
	return ctx != (context.Background())
}

func workOn(context.Context) error { return nil }

func rootSilentGuardReturn(ctx context.Context) error {
	if ctx != nil {
		return workOn(ctx)
	}
	ctx = context.Background()
	return workOn(ctx)
}

type rootOptions struct{ Ctx context.Context }

func rootSilentLocalFallback(ctx context.Context, opt rootOptions) error {
	c := opt.Ctx
	if c == nil {
		c = context.Background()
	}
	return workOn(c)
}

func rootReportedLandElse(ctx context.Context, flag bool) {
	if ctx != nil && flag {
		use(ctx)
	} else {
		ctx = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	}
	use(ctx)
}

func rootReportedOtherTarget(ctx context.Context) error {
	var c context.Context
	if c == nil {
		c, ctx = nil, context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	}
	if err := workOn(c); err != nil {
		return err
	}
	return workOn(ctx)
}

func rootReportedLiveAfterGuard(ctx, parent context.Context) error {
	if ctx != nil {
		return workOn(ctx)
	}
	ctx = parent
	ctx = context.Background() // want "a context is in scope, and this root context drops its cancellation and deadline"
	return workOn(ctx)
}
