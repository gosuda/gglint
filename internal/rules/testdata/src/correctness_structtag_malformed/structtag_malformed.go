// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/structtag_malformed_go.go.

// errorcheck

// correctness::structtag_malformed on a Go package: validates struct field tag
// literals against reflection conventions defined by reflect.StructTag.
// In Go packages, forbid lowers to deny, so //logos:allow can silence it.

package p

type BadTags struct {
	Name   string `json:name,omitempty`     // want `struct tag .json:name,omitempty. cannot be parsed by reflect\.StructTag\.Get: bad syntax for struct tag value`
	Age    int32  `json:"age" json:"years"` // want `struct tag .json:"age" json:"years". cannot be parsed by reflect\.StructTag\.Get: duplicate key "json" in struct tag`
	secret string `json:"secret"`           // want `unexported field secret has tag key json: encoding/json ignores unexported fields`
}

type CleanTags struct {
	Name    string `json:"name,omitempty" xml:"name"`
	ignored string `json:"-"`
}
