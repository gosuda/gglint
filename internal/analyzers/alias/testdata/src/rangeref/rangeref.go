package rangeref

type item struct{ n int }

func RangerefReported() []*item {
	items := []item{{n: 1}, {n: 2}}
	var ptrs []*item
	for _, v := range items {
		ptrs = append(ptrs, &v) // want `this is the address of the per-iteration copy v, not of the slice element`
	}
	return ptrs
}

func RangerefReportedField() []*int {
	items := []item{{n: 1}, {n: 2}}
	var ptrs []*int
	for _, v := range items {
		ptrs = append(ptrs, &v.n) // want `this is the address of the per-iteration copy v, not of the slice element`
	}
	return ptrs
}

func RangerefReportedSend() {
	items := []item{{n: 1}, {n: 2}}
	ch := make(chan *item, len(items))
	for _, v := range items {
		ch <- &v // want `this is the address of the per-iteration copy v, not of the slice element`
	}
}

func RangerefReportedStoreField() {
	items := []item{{n: 1}, {n: 2}}
	hold := struct{ p *item }{}
	for _, v := range items {
		hold.p = &v // want `this is the address of the per-iteration copy v, not of the slice element`
	}
	_ = hold
}

func RangerefReportedStoreMap() {
	items := []item{{n: 1}, {n: 2}}
	m := map[int]*item{}
	for _, v := range items {
		m[v.n] = &v // want `this is the address of the per-iteration copy v, not of the slice element`
	}
	_ = m
}

func RangerefReportedStoreOutside() *item {
	items := []item{{n: 1}, {n: 2}}
	var out *item
	for _, v := range items {
		out = &v // want `this is the address of the per-iteration copy v, not of the slice element`
	}
	return out
}

func RangerefReportedReturn() *item {
	items := []item{{n: 1}, {n: 2}}
	for _, v := range items {
		if v.n > 0 {
			return &v // want `this is the address of the per-iteration copy v, not of the slice element`
		}
	}
	return nil
}

func RangerefSilent() []*item {
	items := []item{{n: 1}, {n: 2}}
	var ptrs []*item
	for i := range items {
		ptrs = append(ptrs, &items[i])
	}
	return ptrs
}

func RangerefSilentIndexed() []*item {
	items := []item{{n: 1}, {n: 2}}
	var ptrs []*item
	for _, v := range items {
		_ = items[0]
		ptrs = append(ptrs, &v)
	}
	return ptrs
}

func RangerefSilentInts(ints []int) []int {
	var out []int
	for _, v := range ints {
		out = append(out, v)
	}
	return out
}

func RangerefSilentBlank() {
	items := []item{{n: 1}, {n: 2}}
	for _, _ = range items {
	}
}
