package nilop

func skippedAndReceive() bool {
	var ch chan bool
	return false && <-ch
}

func skippedOrReceive() bool {
	var ch chan bool
	return true || <-ch
}

func skippedConstantReceive() bool {
	const ready = 2 < 3
	var ch chan bool
	return (!ready) && <-ch
}

func skippedSendValue() {
	var ch chan bool
	out := make(chan bool, 1)
	out <- true || <-ch
}

func evaluatedAndReceive() bool {
	var ch chan bool
	return true && <-ch // want `ch is declared and never made`
}

func evaluatedOrReceive() bool {
	var ch chan bool
	return false || <-ch // want `ch is declared and never made`
}

func conditionalReceive(ready bool) bool {
	var ch chan bool
	return ready && <-ch // want `ch is declared and never made`
}

func shadowedTruthReceive(true bool) bool {
	var ch chan bool
	return true || <-ch // want `ch is declared and never made`
}

func comparisonReceive() bool {
	var ch chan bool
	return true == <-ch // want `ch is declared and never made`
}
