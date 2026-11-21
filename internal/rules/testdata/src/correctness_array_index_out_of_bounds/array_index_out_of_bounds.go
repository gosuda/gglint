// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/array_index_out_of_bounds_go.go.

// errorcheck

package p

type token uint8

const (
	tokenLight token = iota
	tokenDark
	tokenCount
)

func offByOne(t token) string {
	names := [...]string{"light", "dark"}
	if t > tokenCount {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func constantOnLeft(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func inclusiveSafe(t token) string {
	names := [...]string{"light", "dark"}
	if t > tokenDark {
		return "unknown"
	}
	return names[t]
}

func largerBound(t token) string {
	names := [...]string{"light", "dark"}
	if t >= tokenCount+1 {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func signedSafe(i int8) string {
	names := [...]string{"light", "dark"}
	if i < 0 || i >= 2 {
		return "unknown"
	}
	return names[i]
}

func signedNegative(i int8) string {
	names := [...]string{"light", "dark"}
	if i >= 2 {
		return "unknown"
	}
	return names[i] // want "G602: slice index out of range"
}

func fullWidth(t token) string {
	var names [256]string
	return names[t]
}

func unknownGuard(t token, valid bool) string {
	names := [...]string{"light", "dark"}
	if valid {
		return names[t]
	}
	return "unknown"
}

func changedIndex(t token) string {
	names := [...]string{"light", "dark"}
	t = tokenDark
	return names[t]
}
