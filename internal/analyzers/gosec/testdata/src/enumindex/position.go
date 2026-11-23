package enumindex

// Line directives make distinct accesses share a diagnostic location.
func AmbiguousPosition(t token) string {
	names := [...]string{"light", "dark"}
	if t >= tokenCount {
//line collisions.go:8
		return names[t] // want "G602: slice index out of range"
	}
//line collisions.go:8
	return names[t]
}
