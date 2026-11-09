// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/serialized_unsized_int.lgo.

package p

type Wire struct {
	ID     int     `json:"id"`    // want "field ID has platform-dependent type int"
	Count  uint    `json:"count"` // want "field Count has platform-dependent type uint"
	Name   string  `json:"name"`
	Small  int32   `json:"small"`
	Big    int64   `json:"big"`
	Wide   uint64  `json:"wide"`
	Narrow uint16  `json:"narrow"`
	Ratio  float64 `json:"ratio"`
	Tiny   int8    `json:"tiny"`
	Skip   int     `json:"-"` // want "field Skip has platform-dependent type int"
	Plain  int     // want "field Plain has platform-dependent type int"
	Ptr    *int    `json:"ptr"`
	List   []int   `json:"list"`
	Index  uintptr `json:"index"`
	Byte   byte    `json:"byte"`
	Rune   rune    `json:"rune"`
	hidden int     // want "field hidden has platform-dependent type int"
}

type TagsNone struct {
	N int
	M uint
}

type TagsOtherKey struct {
	N int `yaml:"n"`
	M int `xml:"m"`
}

type TagsOne struct {
	N int    // want "field N has platform-dependent type int"
	S string `json:"s"`
}

type TagsTwo struct {
	N uint   // want "field N has platform-dependent type uint"
	S string `json:"s"`
	T string `json:"t"`
}

type TagOptions struct {
	N int `json:"n,omitempty"`       // want "field N has platform-dependent type int"
	M int `yaml:"m" json:"m,string"` // want "field M has platform-dependent type int"
}

type Shared struct {
	A, B int   `json:"ab"` // want "field A has platform-dependent type int" "field B has platform-dependent type int"
	C, D int32 `json:"cd"`
}

type IntAlias = int

type Level int

type Kinds struct {
	A IntAlias `json:"a"` // want "field A has platform-dependent type int"
	L Level    `json:"l"`
}

type Outer struct {
	Inner struct {
		N int `json:"n"` // want "field N has platform-dependent type int"
	} `json:"inner"`
	Bare struct {
		N int
	}
	Wire32 int32 `json:"wire32"`
}

var anon struct {
	N int `json:"n"` // want "field N has platform-dependent type int"
}

func local() int {
	type scratch struct {
		N uint `json:"n"` // want "field N has platform-dependent type uint"
	}
	var s scratch
	return int(s.N)
}

type Generic[T int | int64] struct {
	V T   `json:"v"`
	N int `json:"n"` // want "field N has platform-dependent type int"
}

type Embedded struct {
	int `json:"n"` // want "field int has platform-dependent type int"
	S   string     `json:"s"`
}

type EmbeddedText struct {
	string `json:"s"`
	N      int32 `json:"n"`
}

type EmbeddedUntagged struct {
	int
	S string
}
