package nesting

func flat(n int) int {
	return n
}

func depthThree(n int) int {
	for i := range n {
		switch {
		case i > 1:
			if i > 2 {
				return i
			}
		}
	}
	return 0
}

func depthFour(n int) int { // want `^function depthFour is too complex: nesting depth 4 exceeds 3$`
	for i := range n {
		switch {
		case i > 1:
			if i > 2 {
				for range i {
					return i
				}
			}
		}
	}
	return 0
}

func typeSwitchSelect(v any, c chan int) int { // want `^function typeSwitchSelect is too complex: nesting depth 4 exceeds 3$`
	switch v.(type) {
	case int:
		select {
		case x := <-c:
			for x > 0 {
				if x > 1 {
					return x
				}
				x--
			}
		default:
		}
	}
	return 0
}

func elseIfKeepsDepth(n int) int {
	for i := range n {
		if i == 1 {
			return 1
		} else if i == 2 {
			return 2
		} else if i == 3 {
			if n > 4 {
				return 3
			}
		}
	}
	return 0
}

func literalNotNested(n int) func() int {
	if n > 0 {
		for range n {
			return func() int {
				if n > 1 {
					for range n {
						return 1
					}
				}
				return 0
			}
		}
	}
	return nil
}

var deepLiteral = func(n int) int { // want `^function literal is too complex: nesting depth 4 exceeds 3$`
	for range n {
		for range n {
			for range n {
				if n > 0 {
					return 1
				}
			}
		}
	}
	return 0
}
