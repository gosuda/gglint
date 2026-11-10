package rangemutate

func RangemutateReported() []int {
	s := []int{1, -2, 3}
	for _, x := range s {
		if x > 0 {
			s = append(s, -x) // want `appending to s while ranging over it leaves the loop on the old length and can reallocate`
		}
	}
	return s
}

func RangemutateReportedNestedFor() []int {
	s := []int{1, 2, 3}
	for _, x := range s {
		for y := 0; y < x; y++ {
			s = append(s, y) // want `appending to s while ranging over it leaves the loop on the old length and can reallocate`
		}
	}
	return s
}

func RangemutateReportedSwitch() []int {
	s := []int{1, 2, 3}
	for _, x := range s {
		switch x {
		case 1:
			s = append(s, x) // want `appending to s while ranging over it leaves the loop on the old length and can reallocate`
		}
	}
	return s
}

func RangemutateReportedSelect(ch chan int) []int {
	s := []int{1, 2, 3}
	for _, x := range s {
		select {
		case v := <-ch:
			s = append(s, v) // want `appending to s while ranging over it leaves the loop on the old length and can reallocate`
		default:
			_ = x
		}
	}
	return s
}

func RangemutateSilent() []int {
	s := []int{1, -2, 3}
	out := []int{}
	for _, x := range s {
		if x > 0 {
			out = append(out, -x)
		}
	}
	return out
}

func RangemutateSilentBreak() []int {
	s := []int{1, 2, 3}
	for _, x := range s {
		s = append(s, x)
		break
	}
	return s
}

func RangemutateSilentReturn() []int {
	s := []int{1, 2, 3}
	for _, x := range s {
		s = append(s, x)
		return s
	}
	return s
}

func RangemutateSilentParam(s []int) []int {
	for _, x := range s {
		s = append(s, x)
	}
	return s
}

func RangemutateSilentMap(m map[string]int) {
	for _, x := range m {
		_ = x
	}
}
