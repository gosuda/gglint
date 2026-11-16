package bodies

func communicationBody(in, out chan int) {
	select {
	case <-in:
		out <- 1 // want "channel send can block forever"
	default:
	}
}

func defaultBody(in, out chan int) {
	select {
	case <-in:
	default:
		out <- 1 // want "channel send can block forever"
		<-in     // want "channel receive can block forever"
	}
}

func nestedUnbounded(outer, in, out chan int) {
	select {
	case <-outer:
		select {
		case <-in: // want "channel receive can block forever"
		case out <- 1: // want "channel send can block forever"
		}
	default:
	}
}

func nestedDefault(outer, inner chan int) {
	select {
	case <-outer: // want "channel receive can block forever"
		select {
		case inner <- 1:
		default:
		}
	}
}

func afterSelect(in, out chan int) {
	select {
	case <-in:
	default:
	}
	out <- 1 // want "channel send can block forever"
	<-in     // want "channel receive can block forever"
}

func closureBody(in, out chan int) {
	select {
	case <-in:
		func() {
			out <- 1 // want "channel send can block forever"
		}()
	default:
	}
}
