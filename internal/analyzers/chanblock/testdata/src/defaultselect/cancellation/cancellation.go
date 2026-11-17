package cancellation

import "context"

func doneStillBounds(ctx context.Context, ch chan int) {
	select {
	case ch <- 1:
	case <-ctx.Done():
	}
}

func noBound(in, out chan int) {
	select {
	case <-in: // want "channel receive can block forever"
	case out <- 1: // want "channel send can block forever"
	}
}

func doneBody(ctx context.Context, in, out chan int) {
	select {
	case <-in:
		out <- 1 // want "channel send can block forever"
	case <-ctx.Done():
	}
}

func doneOperands(ctx context.Context, in, out chan int) {
	select {
	case out <- <-in: // want "channel receive can block forever"
	case <-ctx.Done():
	}
}

func doneDoesNotBoundNested(ctx context.Context, outer, inner chan int) {
	select {
	case <-ctx.Done():
	case <-outer:
		select {
		case <-inner: // want "channel receive can block forever"
		}
	}
}
