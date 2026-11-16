package accounting

func selectSendsStillCount() {
	ch := make(chan int, 1)
	ch <- 1 // want "channel send can block forever"
	select {
	case ch <- 2:
	default:
	}
	<-ch // want "channel receive can block forever"
}

func loopSendsStillCount() {
	ch := make(chan int, 1)
	for i := 0; i < 1; i++ {
		select {
		case ch <- i:
		default:
		}
	}
	<-ch // want "channel receive can block forever"
}

func bufferProofUnchanged() {
	ch := make(chan int, 2)
	select {
	case ch <- 1:
	default:
	}
	ch <- 2
	<-ch
}
