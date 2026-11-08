package e

func Race() {
	s := "a"
	go func() {
		println(s)
	}()
	s = "b"
}
