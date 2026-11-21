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

// A remapped line beyond the file length must still report the access.
func RemappedConstLeft(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
//line original.go:120:1
	return names[t] // want "G602: slice index out of range"
}

// A remapped directive still suppresses by its adjusted span.
func RemappedSuppressed(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
//line suppressed.go:200
	// #nosec G602
	return names[t]
}
