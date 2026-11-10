package rules

import (
	"slices"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func TestGosecExcludesTheStandardSkipListBelowStrict(t *testing.T) {
	t.Parallel()
	if got, want := gosecExcluded(config.Strict), []string{"G103", "G104"}; !slices.Equal(got, want) {
		t.Errorf("strict gosecExcluded = %q, want %q", got, want)
	}
	wantBelow := []string{"G101", "G103", "G104", "G115", "G204", "G301", "G302", "G304", "G306", "G404"}
	for _, m := range []config.Mode{config.Standard, config.Basic} {
		if got := gosecExcluded(m); !slices.Equal(got, wantBelow) {
			t.Errorf("mode %v: gosecExcluded = %q, want %q", m, got, wantBelow)
		}
	}
}
