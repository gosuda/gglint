// want package:"package consts"
package consts

import "checkconst/layout"

var _ = layout.PageSize

type record struct {
	id   uint32
	kind uint8
}

// +checkconst checkconst/layout PageSize
const pageSize = 4096

// +checkconst layout PageSize
const shortPathPageSize = 8192 // want `got value "8192", wanted "4096"`

// +checkalign checkconst/layout Header
const headerAlign = 4

// +checkalign checkconst/layout Header
const wrongHeaderAlign = 8 // want `got value "8", wanted "4"`

// +checkoffset checkconst/layout Header.Flags
const flagsOffset = 4

// +checkoffset checkconst/layout Header.Kind
const wrongKindOffset = 4 // want `got value "4", wanted "6"`

// +checksize checkconst/layout Header
const headerSize = 8

// +checksize . record
const recordSize = 8

// +checksize . record
const wrongRecordSize = 5 // want `got value "5", wanted "8"`

const missingName = 1 // +checkconst checkconst/layout Missing // want `fact of type const unavailable for "Missing"`

const unknownPackage = 1 // +checkconst nosuch PageSize // want `unable to resolve package "nosuch"`

const malformed = 1 // +checksize Header // want "potentially malformed checkconst directives"

// +checkconst checkconst/layout PageSize
// +checksize checkconst/layout Header // want "multiple conflicting values"
const conflicting = 4096
