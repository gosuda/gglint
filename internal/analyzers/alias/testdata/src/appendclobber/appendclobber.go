package appendclobber

func AppendclobberReported() int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberReportedDefine() int {
	a := []int{1, 2, 3}
	b := a[:2]
	c := append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	_ = c
	return a[2]
}

func AppendclobberReportedVar() int {
	a := []int{1, 2, 3}
	var b = a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberSilent() int {
	a := []int{1, 2, 3}
	b := a[:2:2]
	b = append(b, 9)
	c := a[:len(a)]
	c = append(c, 9)
	return a[2]
}

func AppendclobberSilentNoLaterUse() int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, 9)
	return b[0]
}

func AppendclobberSilentParam(a []int) []int {
	b := a[:2]
	b = append(b, 9)
	return b
}

func AppendclobberSilentReassigned() int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = a[:1]
	b = append(b, 9)
	return a[2]
}

func AppendclobberSilentWhole() int {
	a := []int{1, 2, 3}
	b := a[:]
	b = append(b, 9)
	return len(b)
}
