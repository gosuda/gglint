package rangeref

import "fmt"

type embeddedRBase struct{ n int }

type embeddedROuter struct{ *embeddedRBase }

type item struct{ n int }

type itemStore struct{ items []item }

func RangerefReported() []item {
	items := []item{{n: 1}, {n: 2}}
	var ptrs []*item
	for _, v := range items {
		ptrs = append(ptrs, &v) // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
	}
	for _, p := range ptrs {
		p.n *= 10
	}
	return items
}

func RangerefReportedField() []item {
	items := []item{{n: 1}, {n: 2}}
	var ptrs []*int
	for _, v := range items {
		ptrs = append(ptrs, &v.n) // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
	}
	for _, p := range ptrs {
		*p = 0
	}
	return items
}

func RangerefReportedSend() []item {
	items := []item{{n: 1}, {n: 2}}
	ch := make(chan *item, len(items))
	for _, v := range items {
		ch <- &v // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
	}
	close(ch)
	for p := range ch {
		p.n *= 10
	}
	return items
}

func RangerefReportedStoreField() []item {
	items := []item{{n: 1}, {n: 2}}
	hold := struct{ p *item }{}
	for _, v := range items {
		hold.p = &v // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
	}
	hold.p.n = 0
	return items
}

func RangerefReportedStoreMap() []item {
	items := []item{{n: 1}, {n: 2}}
	m := map[int]*item{}
	for _, v := range items {
		m[v.n] = &v // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
	}
	for _, p := range m {
		p.n *= 10
	}
	return items
}

func RangerefReportedStoreOutside() []item {
	items := []item{{n: 1}, {n: 2}}
	var out *item
	for _, v := range items {
		out = &v // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
	}
	out.n = 0
	return items
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

func RangerefSilentInts(ints []int) []*int {
	var out []*int
	for _, v := range ints {
		out = append(out, &v)
	}
	return out
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

func RangerefReportedConditionalKill(items []item, keep bool, other *item) []item {
	var out *item
	for _, v := range items {
		out = &v // want `this is the address of the per-iteration copy v, not of the slice element, so a write through it does not reach the slice element`
		if keep {
			out = other
		}
	}
	out.n = 0
	return items
}

func RangerefSilentBranchKill(items []item, keep bool, other *item) *item {
	var out *item
	for _, v := range items {
		out = &v
		if keep {
			out = other
			out.n = 0
		}
	}
	return out
}
func RangerefSilentEmbeddedPtr(items []embeddedROuter) []*int {
	var out []*int
	for _, v := range items {
		out = append(out, &v.n)
	}
	return out
}

func RangerefSilentIndexedFilter(items []item) []*item {
	var ptrs []*item
	for i, v := range items {
		if items[i].n > 0 {
			ptrs = append(ptrs, &v)
		}
	}
	return ptrs
}

func RangerefSilentLocalThrough(items []item) int {
	n := 0
	for _, v := range items {
		var p *item
		p = &v
		p.n = 1
		n += p.n
	}
	return n
}
