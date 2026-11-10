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

type streamRange struct{ ch chan int }

func closeLaterRange(s *streamRange) { close(s.ch) }

func rangeunclosedSilentField() {
	s := &streamRange{ch: make(chan int)}
	go closeLaterRange(s)
	for v := range s.ch {
		_ = v
	}
}

func rangeunclosedSilentReturn() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	for v := range ch {
		useRange(v)
		return
	}
}

func rangeunclosedSilentGoto() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	for v := range ch {
		useRange(v)
		if v > 0 {
			goto doneRange
		}
	}
doneRange:
}

func rangeunclosedSilentRich() {
	ch := make(chan int)
	go func() { ch <- 1; close(ch) }()
	var y = 1
	_ = y
	_ = &ch
	for v := range ch {
		switch v {
		case 1:
			return
		default:
			_ = v
		}
	}
}

func rangeunclosedSilentComposite() {
	ch := make(chan int)
	holders := []chan int{ch}
	go func() { close(holders[0]) }()
	for v := range ch {
		_ = v
	}
}

func rangeunclosedSilentMapKey() {
	ch := make(chan int)
	m := map[chan int]bool{}
	m[ch] = true
	go func() {
		for k := range m {
			close(k)
		}
	}()
	for v := range ch {
		_ = v
	}
}

func genRangeClose() chan int {
	ch := make(chan int)
	close(ch)
	return ch
}

func rangeunclosedSilentCallOrigin() {
	ch := genRangeClose()
	for v := range ch {
		_ = v
	}
}

func rangeunclosedSilentAlias() {
	ch := make(chan int)
	done := ch
	go func() { close(done) }()
	for v := range ch {
		_ = v
	}
}

func rangeunclosedSilentReassign(src chan int) {
	ch := make(chan int)
	ch = src
	for v := range ch {
		useRange(v)
	}
}

func rangeunclosedSilentLabeledContinue() int {
	ch := make(chan int)
	go func() { ch <- 1; ch <- 1 }()
OuterRange:
	for i := 0; i < 2; i++ {
		for v := range ch {
			if v > 0 {
				continue OuterRange
			}
		}
	}
	return 0
}

func rangeunclosedReportedContinueSelf() {
	ch := make(chan int)
	go func() { ch <- 1 }()
LRange:
	for v := range ch { // want `no code closes ch, so this range never ends: close it when the senders finish`
		if v > 0 {
			continue LRange
		}
	}
}
