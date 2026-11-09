package config

import (
	"fmt"
	"slices"
)

type Mode int

const (
	Basic Mode = iota
	Standard
	Strict
)

var modeNames = [...]string{Basic: "basic", Standard: "standard", Strict: "strict"}

func ParseMode(s string) (Mode, error) {
	if i := slices.Index(modeNames[:], s); i >= 0 {
		return Mode(i), nil
	}
	return 0, fmt.Errorf("unknown mode %q (want basic, standard, or strict)", s)
}

func (m Mode) String() string {
	if m < Basic || m > Strict {
		return fmt.Sprintf("Mode(%d)", int(m))
	}
	return modeNames[m]
}

func (m Mode) MarshalText() ([]byte, error) {
	return []byte(m.String()), nil
}

func (m *Mode) UnmarshalText(text []byte) (err error) {
	*m, err = ParseMode(string(text))
	return err
}
