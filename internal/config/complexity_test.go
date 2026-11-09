package config_test

import (
	"reflect"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func TestComplexityForEqualsReadmeTableForEveryMetric(t *testing.T) {
	t.Parallel()
	if n := reflect.TypeFor[config.Complexity]().NumField(); n != len(readmeLimits) || n != len(limitFields) {
		t.Fatalf("Complexity has %d limits, README documents %d", n, len(readmeLimits))
	}
	for _, m := range allModes {
		if got, want := config.ComplexityFor(m), readmeComplexity(m); got != want {
			t.Errorf("ComplexityFor(%v) = %+v, want %+v", m, got, want)
		}
	}
}

func noLooser(tight, loose int) bool {
	if loose == 0 {
		return true
	}
	return tight != 0 && tight <= loose
}

func TestComplexityLimitsNeverLoosenAsModeRises(t *testing.T) {
	t.Parallel()
	pairs := [][2]config.Mode{{config.Basic, config.Standard}, {config.Standard, config.Strict}, {config.Basic, config.Strict}}
	for key, field := range limitFields {
		for _, pair := range pairs {
			lo, hi := config.ComplexityFor(pair[0]), config.ComplexityFor(pair[1])
			if !noLooser(*field(&hi), *field(&lo)) {
				t.Errorf("%s: %v limit %d is looser than %v limit %d (0 is unlimited)", key, pair[1], *field(&hi), pair[0], *field(&lo))
			}
		}
	}
}
