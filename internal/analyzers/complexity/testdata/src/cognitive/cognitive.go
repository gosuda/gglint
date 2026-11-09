package cognitive

func fiveIfs(a, b, c, d, e bool) int {
	n := 0
	if a {
		n++
	}
	if b {
		n++
	}
	if c {
		n++
	}
	if d {
		n++
	}
	if e {
		n++
	}
	return n
}

func sixIfs(a, b, c, d, e, f bool) int { // want `^function sixIfs is too complex: cognitive score 6 exceeds 5$`
	n := 0
	if a {
		n++
	}
	if b {
		n++
	}
	if c {
		n++
	}
	if d {
		n++
	}
	if e {
		n++
	}
	if f {
		n++
	}
	return n
}

func nestedSix(a bool, n int) int { // want `^function nestedSix is too complex: cognitive score 6 exceeds 5$`
	if a {
		for i := range n {
			if i > 1 {
				return i
			}
		}
	}
	return 0
}

func elseIfFive(n int) int {
	if n == 1 {
		return 1
	} else if n == 2 {
		return 2
	} else if n == 3 {
		return 3
	} else if n == 4 {
		return 4
	} else if n == 5 {
		return 5
	} else {
		return 0
	}
}

func elseIfSix(n int) int { // want `^function elseIfSix is too complex: cognitive score 6 exceeds 5$`
	if n == 1 {
		return 1
	} else if n == 2 {
		return 2
	} else if n == 3 {
		return 3
	} else if n == 4 {
		return 4
	} else if n == 5 {
		return 5
	} else if n == 6 {
		return 6
	}
	return 0
}

func chainFive(a, b, c, d bool) bool {
	return a && b || c && d || a && b || c
}

func chainSix(a, b, c, d bool) bool { // want `^function chainSix is too complex: cognitive score 6 exceeds 5$`
	return a && b || c && d || a && b || c && d
}

func sameOperatorChain(a, b, c, d bool) bool {
	return a && b && c && d && a && b && c && d
}

func parenthesizedChains(a, b, c, d bool) bool {
	return (a || b) && (c || d) && (a || b) && (c || d) && !(a || b)
}

func switchFive(n int) int {
	switch n {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	default:
		return 0
	}
}

func switchInLoop(n int) int { // want `^function switchInLoop is too complex: cognitive score 7 exceeds 5$`
	total := 0
	for i := range n {
		switch i {
		case 1:
			total++
		case 2:
			total--
		}
	}
	return total
}

func typeSwitchAndSelect(v any, c chan int) int {
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	default:
	}
	select {
	case x := <-c:
		return x
	default:
		return 0
	}
}

func selectInIf(ok bool, c, d chan int) int { // want `^function selectInIf is too complex: cognitive score 7 exceeds 5$`
	if ok {
		select {
		case x := <-c:
			return x
		case y := <-d:
			return y
		}
	}
	return 0
}

func labeledFive(n int) int {
	total := 0
outer:
	for i := range n {
		total += i
		continue outer
	}
	for range n {
		break
	}
	goto done
done:
	return total
}

func labeledSix(n int) int { // want `^function labeledSix is too complex: cognitive score 6 exceeds 5$`
	total := 0
outer:
	for i := range n {
		for range i {
			break outer
		}
		total += i
	}
	return total
}

func outerFive(a, b, c, d bool) func() int {
	n := 0
	if a {
		n++
	}
	if b {
		n++
	}
	if c {
		n++
	}
	if d {
		n++
	}
	if n > 0 {
		return func() int {
			if n > 1 {
				return 1
			}
			return 0
		}
	}
	return nil
}

func outerOne(n int) func() int {
	if n > 0 {
		return func() int { // want `^function literal is too complex: cognitive score 6 exceeds 5$`
			for range n {
				if n > 1 {
					for range n {
						return 1
					}
				}
			}
			return 0
		}
	}
	return nil
}

func elseIfNestedFive(n int) int {
	for i := range n {
		if i == 1 {
			return 1
		} else if i == 2 {
			return 2
		}
	}
	return 0
}

func elseIfNestedSeven(n int) int { // want `^function elseIfNestedSeven is too complex: cognitive score 7 exceeds 5$`
	for i := range n {
		if i == 1 {
			return 1
		} else if i == 2 {
			return 2
		} else if i == 3 {
			return 3
		}
	}
	return 0
}

func elseIfBodyFive(a, b, c, d bool) int {
	n := 0
	if a {
		n++
	} else if b {
		if c {
			n++
		}
	}
	if d {
		n++
	}
	return n
}

func chainInLoopFive(a, b, c bool, n int) int {
	total := 0
	for range n {
		if a && b || c && a {
			total++
		}
	}
	return total
}

func chainInLoopSix(a, b, c bool, n int) int { // want `^function chainInLoopSix is too complex: cognitive score 6 exceeds 5$`
	total := 0
	for range n {
		if a && b || c && a || b {
			total++
		}
	}
	return total
}
