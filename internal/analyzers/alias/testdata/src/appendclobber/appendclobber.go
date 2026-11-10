package appendclobber

func AppendclobberStub() []int

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

func AppendclobberSilentVarForm() int {
	a := []int{1, 2, 3}
	var b = a[:2]
	b = append(b, 9)
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

func AppendclobberSilentRealloc() int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, 9, 10)
	return a[2]
}

func AppendclobberSilentReassignedA() int {
	a := make([]int, 4)
	b := a[:2]
	a = make([]int, 4)
	b = append(b, 9)
	return a[2]
}

func AppendclobberSilentWriteOnlyLocal() int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, 9)
	a = nil
	return len(b)
}

func AppendclobberSilentUnreachable(cond bool) int {
	a := []int{1, 2, 3}
	b := a[:2]
	if cond {
		b = append(b, 9)
		return len(b)
	}
	return a[2]
}

func AppendclobberReportedSpread(src []int) int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, src...) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberReportedMakeNonConst(n int) int {
	a := make([]int, 3, n)
	b := a[:2]
	b = append(b, 9, 10) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberReportedArray() int {
	a := [5]int{}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberReportedKeyed() int {
	a := []int{4: 0}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberReportedIndexRead(m map[int]int) {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	m[a[2]] = 1
}

func AppendclobberReportedGrown() int {
	a := make([]int, 3)
	a = append(a, 1, 2, 3)
	b := a[:2]
	b = append(b, 9, 10, 11) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

