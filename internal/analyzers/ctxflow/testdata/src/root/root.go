package root

import (
	"context"
	"net/http"
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
	f := func(ctx context.Context) { use(context.Background()) }
	f(nil)
}

func init() {
	use(context.Background())
	g := func(ctx context.Context) { use(context.TODO()) }
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
	f := context.Background
	use(f())
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

func rootReportedNestedArg(ctx context.Context, k, v any) context.Context {
	if ctx == nil {
		ctx = context.WithValue(context.Background(), k, v) // want "a context is in scope, and this root context drops its cancellation and deadline"
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
