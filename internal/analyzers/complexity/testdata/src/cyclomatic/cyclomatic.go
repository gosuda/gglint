package cyclomatic

func straight() int {
	return 1
}

func cycFour(a, b bool, n int) int {
	total := 0
	if a && b {
		total++
	}
	for i := range n {
		total += i
	}
	return total
}

func cycFive(a, b, c bool, n int) int { // want `^function cycFive is too complex: cyclomatic complexity 5 exceeds 4$`
	total := 0
	if a && b || c {
		total++
	}
	for i := range n {
		total += i
	}
	return total
}

func switchCases(n int) int {
	switch n {
	case 1, 2:
		return 1
	case 3:
		return 2
	case 4:
		return 3
	default:
		return 0
	}
}

func selectFive(a, b, c, d chan int) int { // want `^function selectFive is too complex: cyclomatic complexity 5 exceeds 4$`
	select {
	case x := <-a:
		return x
	case x := <-b:
		return x
	case x := <-c:
		return x
	case x := <-d:
		return x
	default:
		return 0
	}
}

func elseIfFour(n int) int {
	if n == 1 {
		return 1
	} else if n == 2 {
		return 2
	} else if n == 3 {
		return 3
	} else {
		return 0
	}
}

func loopForms(n int) int {
	total := 0
	for {
		break
	}
	for i := 0; i < n; i++ {
		total += i
	}
	for range n {
		total++
	}
	return total
}

func outerTwo(n int) func() bool {
	if n > 0 {
		return func() bool { // want `^function literal is too complex: cyclomatic complexity 5 exceeds 4$`
			return n > 1 && n > 2 && n > 3 && n > 4 && n > 5
		}
	}
	return nil
}

func loopFormsFive(n int) int { // want `^function loopFormsFive is too complex: cyclomatic complexity 5 exceeds 4$`
	total := 0
	for {
		break
	}
	for total < n {
		total++
	}
	for i := 0; i < n; i++ {
		total += i
	}
	for range n {
		total++
	}
	return total
}

func typeSwitchFive(v any) int { // want `^function typeSwitchFive is too complex: cyclomatic complexity 5 exceeds 4$`
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	case bool:
		return 3
	case float64:
		return 4
	default:
		return 0
	}
}

func parenthesizedOpsFive(a, b, c, d bool) int { // want `^function parenthesizedOpsFive is too complex: cyclomatic complexity 5 exceeds 4$`
	if (a || b) && (c || d) {
		return 1
	}
	return 0
}

func jumpsFour(n int) int {
	total := 0
outer:
	for i := range n {
		for range i {
			continue outer
		}
		if i > 2 {
			break outer
		}
	}
	goto done
done:
	return total
}
