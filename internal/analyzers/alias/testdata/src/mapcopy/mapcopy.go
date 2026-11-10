package mapcopy

type embeddedBase struct{ n int }

type embeddedOuter struct{ *embeddedBase }

type item struct{ n int }

type mapHolder struct{ m map[string]item }

func useInt(n int) {}

func MapcopyReported(m map[string]item, k string) {
	v := m[k]
	v.n++ // want `v is a copy of the element of m, so this write is lost`
}

func MapcopyReportedAssign(m map[string]item, k string) {
	v := m[k]
	v.n = 1 // want `v is a copy of the element of m, so this write is lost`
}

func MapcopyReportedDec(m map[string]item, k string) {
	v := m[k]
	v.n-- // want `v is a copy of the element of m, so this write is lost`
}

func MapcopyReportedFieldMap(s *mapHolder, k string) {
	v := s.m[k]
	v.n++ // want `v is a copy of the element of s.m, so this write is lost`
}

func MapcopyReportedReturnField(m map[string]item, k string) int {
	v := m[k]
	v.n++ // want `v is a copy of the element of m, so this write is lost`
	return v.n
}

func MapcopyReportedArgField(m map[string]item, k string) {
	v := m[k]
	v.n++ // want `v is a copy of the element of m, so this write is lost`
	useInt(v.n)
}

func MapcopyReportedRhsField(m map[string]item, k string) {
	v := m[k]
	v.n = v.n + 1 // want `v is a copy of the element of m, so this write is lost`
}

func MapcopySilentEmbeddedPtr(m map[string]embeddedOuter, k string) {
	v := m[k]
	v.n++
}

func MapcopySilent(m map[string]item, k string) {
	v := m[k]
	v.n++
	m[k] = v
}

func MapcopySilentReturned(m map[string]item, k string) item {
	v := m[k]
	v.n++
	return v
}

func MapcopySilentArg(m map[string]item, k string) {
	v := m[k]
	v.n++
	m[k] = useItem(v)
}

func MapcopySilentCopied(m map[string]item, k string) {
	v := m[k]
	v.n++
	w := v
	m[k] = w
}

func MapcopySilentComposite(m map[string]item, k string) struct{ it item } {
	v := m[k]
	v.n++
	return struct{ it item }{it: v}
}

func MapcopySilentAddr(m map[string]item, k string) *item {
	v := m[k]
	v.n++
	return &v
}

func MapcopySilentHeld(m map[string]item, k string) {
	v := m[k]
	v.n++
	held := []item{v}
	m[k] = held[0]
}

type inner struct{ n int }

type withPtr struct{ p *inner }

func MapcopySilentPtrField(m map[string]withPtr, k string) {
	v := m[k]
	v.p.n++
}

func useItem(v item) item { return v }
