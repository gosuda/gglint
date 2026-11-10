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

func AppendclobberSilentVarForm() int {
	a := []int{1, 2, 3}
	var b = a[:2:2]
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
	return len(b)
}

func AppendclobberSilentMakeNonConstTwo(n int) int {
	a := make([]int, 3, n)
	b := a[:2]
	b = append(b, 9, 10)
	return len(b)
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
	return out, len(xs)
}

func AppendclobberSilentInPlaceRange() ([]int, int) {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x)
		}
	}
	return out, len(xs)
}

func AppendclobberReportedInPlaceReadAfter() ([]int, int) {
	xs := []int{2, 4, 1, 8}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x) // want `out shares its backing array with xs, so this append overwrites elements of xs that are read later`
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

func AppendclobberReportedCondCap(c bool) int {
	a := make([]int, 3, 10)
	if c {
		a = a[0:3:3]
	}
	b := a[:2]
	b = append(b, 9, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a[2]
}

func AppendclobberSilentUncondCap() int {
	a := make([]int, 3, 10)
	a = a[0:3:3]
	b := a[:2]
	b = append(b, 9, 9)
	return a[2]
}

func AppendclobberSilentInPlaceTailClear() []int {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x)
		}
	}
	clear(xs[len(out):])
	return out
}

func AppendclobberSilentInPlaceTailLoop() []int {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x)
		}
	}
	for i := len(out); i < len(xs); i++ {
		xs[i] = 0
	}
	return out
}

func AppendclobberReportedWholeSlice() []int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a
}

func AppendclobberReportedRangeAfter() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	s := 0
	for _, x := range a {
		s += x
	}
	return s
}

func AppendclobberSilentLowIndex() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	return a[0]
}

func AppendclobberSilentLen() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	return len(a)
}

func AppendclobberReportedReassignedAfterRead() int {
	a := []int{1, 2, 3}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	x := a[2]
	a = nil
	return x
}

func AppendclobberReportedInPlaceOpAssign() ([]int, int) {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x) // want `out shares its backing array with xs, so this append overwrites elements of xs that are read later`
		}
	}
	xs[0] += 1
	return out, xs[0]
}

func AppendclobberSilentInPlaceTailSlice() ([]int, []int) {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x)
		}
	}
	return out, xs[len(out):]
}

func AppendclobberSilentNilCompare() bool {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	return a == nil
}

func AppendclobberSilentNonConstIndex() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	s := 0
	for i := 0; i < 2; i++ {
		s += a[i]
	}
	return s
}

func AppendclobberSilentCopyDest(src []int) int {
	a := make([]int, 3, 8)
	b := a[:2]
	b = append(b, 9)
	copy(a[2:], src)
	return len(a)
}

func AppendclobberReportedCallSource() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	dst := make([]int, 4)
	copy(dst, a)
	return len(dst)
}

func AppendclobberReportedSend(ch chan []int) {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	ch <- a
}

func AppendclobberSilentLocalAlias() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	var x = a
	return len(x)
}

func AppendclobberReportedAliasRead() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	x := a[2]
	return x
}

func AppendclobberSilentSliceHigh() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	return a[0:1][0]
}

type appendIntSlice []int

func (s appendIntSlice) Sum() int {
	t := 0
	for _, x := range s {
		t += x
	}
	return t
}

func AppendclobberReportedMethodRecv() int {
	a := appendIntSlice{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return a.Sum()
}

func readThird(p *[]int) int { return (*p)[2] }

func AppendclobberReportedAddrTaken() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	return readThird(&a)
}

func AppendclobberSilentAddrLocal() ([]int, int) {
	xs := []int{1, 2, 3, 4}
	out := xs[:0]
	for _, x := range xs {
		if x%2 == 0 {
			out = append(out, x)
		}
	}
	p := &xs
	return out, len(*p)
}

func AppendclobberReportedNamedResult() (r []int) {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
	r = a
	return
}

func AppendclobberReportedCaptured() {
	var keep []int
	f := func() {
		a := []int{1, 2, 3, 4}
		b := a[:2]
		b = append(b, 9) // want `b shares its backing array with a, so this append overwrites elements of a that are read later`
		keep = a
	}
	_ = f
	_ = keep
}

type ints []int

func (r ints) clean() int {
	a := []int{1, 2, 3, 4}
	b := a[:2]
	b = append(b, 9)
	r = a
	return len(r)
}
