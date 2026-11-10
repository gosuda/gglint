package rangeref

import "fmt"

func RangerefStub(items []item)

type item struct{ n int }

type itemStore struct{ items []item }

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
	hold.p.n = 0
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
	out.n = 0
	return out
}

func RangerefSilentReturnAtOnce() *item {
	items := []item{{n: 1}, {n: 2}}
	for _, v := range items {
		if v.n > 0 {
			return &v
		}
	}
	return nil
}

func RangerefSilentStoreBreak() *item {
	items := []item{{n: 1}, {n: 2}}
	var out *item
	for _, v := range items {
		if v.n > 0 {
			out = &v
			break
		}
	}
	return out
}

func RangerefSilentNestedOuter(items []item, valid func(item) bool) *item {
	for _, v := range items {
		for range v.n {
			if valid(v) {
				return &v
			}
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

func RangerefSilentFieldIndex(s *itemStore) []*item {
	var ptrs []*item
	for _, v := range s.items {
		_ = s.items[0]
		ptrs = append(ptrs, &v)
	}
	return ptrs
}

func RangerefSilentNestedCall(items []item) []string {
	var lines []string
	for _, v := range items {
		lines = append(lines, fmt.Sprint(&v))
	}
	return lines
}

func RangerefSilentNestedSend(items []item) {
	ch := make(chan string, len(items))
	for _, v := range items {
		ch <- fmt.Sprint(&v)
	}
}

func RangerefSilentNestedReturn(items []item) string {
	for _, v := range items {
		if v.n > 0 {
			return fmt.Sprint(&v)
		}
	}
	return ""
}

func RangerefSilentSplitAssign(items []item) int {
	count := 0
	for _, v := range items {
		var p *item
		count, p = 1, &v
		_ = p
	}
	return count
}

func RangerefSilentSliceStore(items []item, out []*item) {
	for i, v := range items {
		out[i] = &v
	}
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

type innerBox struct{ n int }

type withPtr struct{ p *innerBox }

func RangerefSilentPtrField(items []withPtr) []*int {
	var out []*int
	for _, v := range items {
		out = append(out, &v.p.n)
	}
	return out
}

func RangerefSilentReassigned(items []item) *item {
	var p *item
	for _, v := range items {
		p = &v
	}
	p = &items[0]
	p.n = 1
	return p
}

func RangerefReportedConditionalKill(items []item, keep bool, other *item) *item {
	var out *item
	for _, v := range items {
		out = &v // want `this is the address of the per-iteration copy v, not of the slice element`
		if keep {
			out = other
		}
	}
	out.n = 0
	return out
}
