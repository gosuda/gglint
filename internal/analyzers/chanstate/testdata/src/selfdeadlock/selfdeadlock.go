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
	go func() { <-ch }()
	ch <- 1
}


func selfdeadlockSilentBranch(a bool) {
	ch := make(chan int)
	go func() {
		if a {
			<-ch
		} else {
			ch <- 1
		}
	}()
	if a {
		ch <- 1
	} else {
		<-ch
	}
}

func peerSelf(c chan int) {
	go func() {
		<-c
		c <- 2
	}()
}

func selfdeadlockSilentEscapeCall() int {
	ch := make(chan int)
	peerSelf(ch)
	ch <- 1
	return <-ch
}


func selfdeadlockSilentAlias() int {
	ch := make(chan int)
	r := ch
	go func() { v := <-r; r <- v + 1 }()
	ch <- 1
	return <-ch
}

func selfdeadlockSilentReassign() int {
	ch := make(chan int)
	ch = make(chan int, 1)
	ch <- 1
	return <-ch
}

func selfdeadlockSilentPtrStore() int {
	ch := make(chan int)
	var slot chan int
	p := &slot
	*p = ch
	go func() { v := <-slot; slot <- v + 1 }()
	ch <- 1
	return <-ch
}

func selfdeadlockSilentDefineReuse(src chan int) int {
	ch := make(chan int)
	n, ch := 1, src
	_ = n
	ch <- 1
	return <-ch
}
