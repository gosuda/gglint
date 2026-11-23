package secposition

import "crypto/md5" // #nosec G501

func Weak(b []byte) [16]byte {
//line ast.go:400:20
	return md5.Sum(b) // want "G401: Use of weak cryptographic primitive"
}

func Bounds(i uint8) string {
	names := [...]string{"light", "dark"}
	if i > 2 {
		return "unknown"
	}
//line bounds.go:1000:40
	return names[i] // want "G602: slice index out of range"
}

func Inline(i uint8) string {
	names := [...]string{"light", "dark"}
	if i > 2 {
		return "unknown"
	}
	return /*line inline.go:1700:6*/ names[i] // want "G602: slice index out of range"
}

func Suppressed(i uint8) string {
	names := [...]string{"light", "dark"}
	if i > 2 {
		return "unknown"
	}
//line hidden.go:700:40
	return names[i] // #nosec G602
}

func Reported(i uint8) string {
	names := [...]string{"light", "dark"}
	if i > 2 {
		return "unknown"
	}
//line shown.go:700:40
	return names[i] // want "G602: slice index out of range"
}

func UnknownColumn(i uint8) string {
	names := [...]string{"light", "dark"}
	if i > 2 {
		return "unknown"
	}
//line unknown.go:2000
	return names[i] // want "G602: slice index out of range"
}
