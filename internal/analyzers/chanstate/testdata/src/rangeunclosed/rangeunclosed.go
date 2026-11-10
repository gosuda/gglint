package rangeunclosed

var rangeSink any

func useRange(v int) { _ = v }

func rangeunclosedReported() {
	ch := make(chan int)
	go func() { ch <- 1 }()
	for v := range ch { // want `no code closes ch, so this range never ends: close it when the senders finish`
		useRange(v)
	}
}

func rangeunclosedSilent() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	for v := range ch {
		useRange(v)
	}
}

func rangeunclosedSilentEscape() {
	ch := make(chan int)
	rangeSink = ch
	for v := range ch {
		useRange(v)
	}
}

func rangeunclosedSilentBreak() {
	ch := make(chan int)
	go func() { ch <- 1 }()
	for v := range ch {
		useRange(v)
		if v > 0 {
			break
		}
	}
}
