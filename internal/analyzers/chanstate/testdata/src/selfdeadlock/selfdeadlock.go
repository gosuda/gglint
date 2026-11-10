package selfdeadlock

func selfdeadlockReported() {
	ch := make(chan int)
	ch <- 1 // want `ch is unbuffered and both ends are in this goroutine, so this operation deadlocks: give it capacity 1`
	<-ch
}

func selfdeadlockSilent() {
	ch := make(chan int, 1)
	ch <- 1
	<-ch
}

func selfdeadlockSilentLiteral() {
	ch := make(chan int)
	go func() { _ = ch }()
	ch <- 1
	<-ch
}

func selfdeadlockSilentSends(xs []int) {
	ch := make(chan int)
	ch <- 1
	ch <- 2
	for range xs {
	}
}

func selfdeadlockSilentBranch(a bool) {
	ch := make(chan int)
	if a {
		ch <- 1
	} else {
		<-ch
	}
}
