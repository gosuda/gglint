package vettool

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

func TestNolintTypecheckTokenDoesNotHideUnusedTokens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		directive string
		want      []string
	}{
		{
			name:      "typecheck beside an unused selected rule",
			directive: "//nolint:typecheck,printf // reason",
			want:      []string{"typecheck findings cannot be suppressed", "unused nolint directive for printf"},
		},
		{
			name:      "typecheck alone",
			directive: "//nolint:typecheck // reason",
			want:      []string{"typecheck findings cannot be suppressed"},
		},
		{
			name:      "unused selected rule alone",
			directive: "//nolint:printf // reason",
			want:      []string{"unused nolint directive for printf"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEngine(t, config.Config{Mode: config.Standard})
			var got []string
			src := "package p\n\nvar X = 1 " + tc.directive + "\n"
			pass := newPass(t, "/work/p/a.go", src, func(d analysis.Diagnostic) { got = append(got, d.Message) })
			if _, err := e.nolintAnalyzer(nil).Run(pass); err != nil {
				t.Fatalf("run nolint: %v", err)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("nolint reported %q, want %q", got, tc.want)
			}
		})
	}
}
