package mapcopy

type item struct{ n int }

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

func MapcopyReportedVar(m map[string]item, k string) {
	var v = m[k]
	v.n++ // want `v is a copy of the element of m, so this write is lost`
}

func MapcopySilent(m map[string]item, k string) {
	v := m[k]
	v.n++
	m[k] = v
}

func MapcopySilentOtherKey(m map[string]item, k string) {
	v := m[k]
	v.n++
	m["other"] = v
}

func MapcopySilentReturned(m map[string]item, k string) item {
	v := m[k]
	v.n++
	return v
}

func MapcopySilentArg(m map[string]item, k string) {
	v := m[k]
	v.n++
	useItem(v)
}

func MapcopySilentCopied(m map[string]item, k string) {
	v := m[k]
	v.n++
	w := v
	_ = w
}

func useItem(v item) {}
