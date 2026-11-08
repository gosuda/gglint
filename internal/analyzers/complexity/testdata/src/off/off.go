package off

type Wide interface {
	A()
	B()
	C()
	D()
	E()
	F()
}

func Everything(a, b, c, d, e, f bool, n int) int {
	total := 0
	for i := range n {
		for j := range i {
			switch {
			case a && b || c && d:
				if e || f {
					for range j {
						total++
					}
				} else if a {
					total--
				}
			}
		}
	}
	return total
}
