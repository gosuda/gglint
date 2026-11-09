// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/string_intconv_go.go.

// errorcheck

// suspicious::string_intconv on a Go package (LINT-RULES.md Sec 1.2): detects
// conversions of the form string(x) where x has an integer type other than
// rune or byte.

package p

import "strconv"

func badConversions(port int, id int64) {
	_ = string(port) // want "yields a string of one rune, not a string of digits"
	_ = string(1234) // want "yields a string of one rune, not a string of digits"
	_ = string(id)   // want "yields a string of one rune, not a string of digits"
}

func formatDecimal(x int) string {
	return strconv.Itoa(x)
}

func cleanConversions(port int, r rune, b byte, bs []byte) {
	_ = strconv.Itoa(port)
	_ = string(r)
	_ = string('A')
	_ = string(b)
	_ = string(rune(port))
	_ = string(bs)
	_ = formatDecimal(port)
}
