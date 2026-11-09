package structtagmalformed

type inner struct{}

type Valid struct {
	Name    string `json:"name,omitempty" xml:"name"`
	Count   int    `json:"count"`
	ignored string `json:"-"`
	other   string `yaml:"other"`
	empty   string `json:""`
	Escaped string "json:\"escaped\""
	Spaced  string `json:"a"  xml:"b"`
	Trail   string `json:"t" `
}

type Embedded struct {
	inner `json:"inner"`
}

type Key struct {
	F int `:"x"` // want `struct tag .:"x". cannot be parsed by reflect\.StructTag\.Get: bad syntax for struct tag key$`
}

type Pair struct {
	F int `json` // want `bad syntax for struct tag pair$`
}

type Unquoted struct {
	F int `json:x` // want `bad syntax for struct tag value$`
}

type Interpreted struct {
	F int "json:x" // want `struct tag .json:x. cannot be parsed by reflect\.StructTag\.Get: bad syntax for struct tag value$`
}

type Unterminated struct {
	F int `json:"x` // want `bad syntax for struct tag value$`
}

type BadEscape struct {
	F int `json:"\q"` // want `bad syntax for struct tag value$`
}

type NoSpace struct {
	F int `json:"a"xml:"b"` // want `key:"value" pairs not separated by spaces$`
}

type Duplicate struct {
	F int `json:"a" json:"b"` // want `duplicate key "json" in struct tag$`
}

type DuplicateXML struct {
	F int `xml:"a" yaml:"b" xml:"c"` // want `duplicate key "xml" in struct tag$`
}

type HiddenJSON struct {
	secret string `json:"secret"` // want `unexported field secret has tag key json: encoding/json ignores unexported fields`
}

type HiddenXML struct {
	name string `xml:"name"` // want `unexported field name has tag key xml: encoding/xml ignores unexported fields`
}

type HiddenBoth struct {
	both string `json:"b" xml:"b"` // want `unexported field both has tag key json:` `unexported field both has tag key xml:`
}

type HiddenPair struct {
	a, b string `json:"x"` // want `unexported field a has tag key json:` `unexported field b has tag key json:`
}

type Blank struct {
	_ int `json:"x"` // want `unexported field _ has tag key json:`
}

type MalformedHidden struct {
	hidden string `json:bad` // want `bad syntax for struct tag value$`
}

func local() any {
	return struct {
		F int `json:x` // want `bad syntax for struct tag value$`
	}{}
}
