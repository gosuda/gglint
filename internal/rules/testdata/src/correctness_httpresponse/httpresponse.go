// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/httpresponse_go.go.

// errorcheck

// correctness::httpresponse on a Go package (LINT-RULES.md Sec 1.2): detects
// body-close deferred before checking HTTP response error.

package p

import "net/http"

func badGet() {
	resp, err := http.Get("http://example.com")
	defer resp.Body.Close() // want "using resp before checking for errors"
	if err != nil {
		return
	}
	_ = resp
}

func goodGet() {
	resp, err := http.Get("http://example.com")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_ = resp
}

func otherDefer() {
	resp, err := http.Get("http://example.com")
	defer cleanup()
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_ = resp
}

func cleanup() {}
