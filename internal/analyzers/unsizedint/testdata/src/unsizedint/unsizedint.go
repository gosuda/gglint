package unsizedint

type count int

type alias = int

type record struct {
	ID int `json:"id"` // want `field ID has platform-dependent type int in a JSON struct: use int64 or int32`

	Total uint // want `field Total has platform-dependent type uint in a JSON struct: use int64 or int32`

	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Small int32
	Width uint32
}

type pair struct {
	A, B int `json:"ab"` // want `field A has platform-dependent type int` `field B has platform-dependent type int`
}

type definedTypes struct {
	N count  `json:"n"`
	P *int   `json:"p"`
	S []int  `json:"s"`
	M uint64 `json:"m"`
}

type aliasField struct {
	A alias `json:"a"` // want `field A has platform-dependent type int`
}

type untagged struct {
	N int
	M uint `xml:"m"`
}

type embeddedInt struct {
	int // want `field int has platform-dependent type int`

	Label string `json:"label"`
}

type embedsRecord struct {
	record
	N int
}

type outer struct {
	Inner struct {
		N int `json:"n"` // want `field N has platform-dependent type int`
	}

	M int
}

type wrapper struct {
	Inner struct {
		N int
	} `json:"inner"`

	Total int // want `field Total has platform-dependent type int`
}

type skipped struct {
	Hidden int `json:"-"` // want `field Hidden has platform-dependent type int`
}

func local() any {
	type event struct {
		Seq uint `json:"seq"` // want `field Seq has platform-dependent type uint`
	}
	return event{}
}
