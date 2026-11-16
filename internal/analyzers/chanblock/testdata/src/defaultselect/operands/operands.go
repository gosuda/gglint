package operands

func sendValue(out, in chan int) {
	select {
	case out <- <-in: // want "channel receive can block forever"
	default:
	}
}

func sendChannel(outputs chan chan int) {
	select {
	case (<-outputs) <- 1: // want "channel receive can block forever"
	default:
	}
}

func receiveChannel(inputs chan chan int) {
	select {
	case <-(<-inputs): // want "channel receive can block forever"
	default:
	}
}

func receiveTarget(in, index chan int, dst []int) {
	select {
	case dst[<-index] = <-in: // want "channel receive can block forever"
	default:
	}
}
