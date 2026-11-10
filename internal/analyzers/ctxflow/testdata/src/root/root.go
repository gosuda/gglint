package root

import "context"

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
}

func init() {
	use(context.Background())
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
