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

func AppendclobberSilentSpread(src []int) int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, src...)
	return a[2]
}

func AppendclobberSilentMakeNonConstTwo(n int) int {
	a := make([]int, 3, n)
	b := a[:2]
	b = append(b, 9, 10)
	return a[2]
}

func AppendclobberReportedMakeNonConstOne(n int) int {
	a := make([]int, 3, n)
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
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

func AppendclobberSilentInPlaceFilter() ([]int, int) {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for i := 0; i < len(xs); i++ {
		if xs[i]%2 == 0 {
			out = append(out, xs[i])
		}
	}
	return out, xs[2]
}

func AppendclobberSilentInPlaceRange() ([]int, int) {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x)
		}
	}
	return out, xs[2]
}

func AppendclobberReportedZeroOutside() int {
	a := []int{1, 2, 3}
	b := a[:0]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[0]
}

func AppendclobberReportedZeroMulti() int {
	a := []int{1, 2, 3, 4}
	b := a[:0]
	for i := 0; i < len(a); i++ {
		if a[i]%2 == 0 {
			b = append(b, a[i], 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
		}
	}
	return a[1]
}

func AppendclobberSilentTwoExclusive() []int {
	a := make([]int, 8)
	out := a[:0]
	for i := 0; i < len(a); i++ {
		if a[i]%2 == 0 {
			out = append(out, a[i])
			continue
		}
		out = append(out, -a[i])
	}
	return out
}

