package config

import "fmt"

type Mode int

const (
	Basic Mode = iota
	Standard
	Strict
)

var modeNames = [...]string{Basic: "basic", Standard: "standard", Strict: "strict"}

func ParseMode(s string) (Mode, error) {
	for m, name := range modeNames {
		if name == s {
			return Mode(m), nil
		}
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
	if m < Basic || m > Strict {
		return nil, fmt.Errorf("invalid mode %d", int(m))
	}
	return []byte(modeNames[m]), nil
}

func (m *Mode) UnmarshalText(text []byte) error {
	parsed, err := ParseMode(string(text))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
