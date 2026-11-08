package config_test

import (
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func TestParseModeAcceptsExactlyTheDocumentedNames(t *testing.T) {
	for _, m := range allModes {
		got, err := config.ParseMode(modeNames[m])
		if err != nil || got != m || got.String() != modeNames[m] {
			t.Errorf("ParseMode(%q) = %v, %v; want %v", modeNames[m], got, err, m)
		}
	}
	for _, name := range invalidModeNames {
		if _, err := config.ParseMode(name); err == nil || err.Error() != unknownMode(name) {
			t.Errorf("ParseMode(%q) error = %v, want %q", name, err, unknownMode(name))
		}
	}
}

func TestModeTextRoundTripsAndRejectsUnknownText(t *testing.T) {
	for _, m := range allModes {
		text, err := m.MarshalText()
		if err != nil || string(text) != modeNames[m] {
			t.Fatalf("%v.MarshalText() = %q, %v; want %q", m, text, err, modeNames[m])
		}
		var back config.Mode
		if err := back.UnmarshalText(text); err != nil || back != m {
			t.Fatalf("UnmarshalText(%q) = %v, %v; want %v", text, back, err, m)
		}
	}
	for _, name := range invalidModeNames {
		var m config.Mode
		if err := m.UnmarshalText([]byte(name)); err == nil || err.Error() != unknownMode(name) {
			t.Errorf("UnmarshalText(%q) error = %v, want %q", name, err, unknownMode(name))
		}
	}
}
