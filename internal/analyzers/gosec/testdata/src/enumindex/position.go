package enumindex

// Line directives make both accesses share a diagnostic location.
func AmbiguousPosition(t token) string {
	names := [...]string{"light", "dark"}
	if t >= tokenCount {
//line collisions.go:8
		return names[t]
	}
//line collisions.go:8
	return names[t] // want "G602: slice index out of range" "G602: slice index out of range"
}
