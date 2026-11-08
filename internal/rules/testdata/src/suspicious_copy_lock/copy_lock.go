// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/copy_lock_go.go.

// errorcheck

// suspicious::copy_lock on a Go package (LINT-RULES.md Sec 1.2): detects
// copying values containing embedded or direct locks. Copying an active
// lock duplicates synchronization state.

package p

import "sync"

type State struct {
	mu    sync.Mutex
	count int
}

func (s State) BadRecv() int { // want "passes lock by value|copies lock value"
	return s.count
}

func (s *State) GoodRecv() int {
	return s.count
}

func passByVal(s State) int { // want "passes lock by value|copies lock value"
	return s.count
}

func passByPtr(s *State) int {
	return s.count
}

func copyAssign() {
	var s1 State
	var s2 State
	s2 = s1     // want "passes lock by value|copies lock value"
	var s3 = s1 // want "passes lock by value|copies lock value"
	_ = &s2
	_ = &s3
}

func copyReturn() State {
	var s State
	return s // want "passes lock by value|copies lock value"
}

func cleanForms() {
	ptr := &State{}
	_ = ptr
	passByPtr(ptr)
}
