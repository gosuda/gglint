package g

func Send() {
	ch := make(chan int)
	ch <- 1
}
