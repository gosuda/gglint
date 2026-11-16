package communications

func send(ch chan int) {
	select {
	case ch <- 1:
	default:
	}
}

func receive(ch chan int) {
	select {
	case <-ch:
	default:
	}
}

func receiveAssign(ch chan int) int {
	select {
	case value := <-ch:
		return value
	default:
		return 0
	}
}

func receiveCommaOK(ch chan int) bool {
	select {
	case _, ok := <-ch:
		return ok
	default:
		return false
	}
}

func nilChannels() {
	var in, out chan int
	select {
	case out <- 1:
	case <-in:
	default:
	}
}

func defaultFirst(ch chan int) {
	select {
	default:
	case ch <- 1:
	}
}

type tokenPool struct {
	tokens chan int
}

func (p *tokenPool) tryReceive() (int, bool) {
	select {
	case token := <-p.tokens:
		return token, true
	default:
		return 0, false
	}
}
