package config_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

type fieldState int

const (
	unset fieldState = iota
	matching
	mismatching
)

func pick(s fieldState, match, mismatch []string) []string {
	switch s {
	case unset:
	case matching:
		return match
	case mismatching:
		return mismatch
	}
	return nil
}

func excludeFor(states [4]fieldState, isTest bool) config.Exclude {
	e := config.Exclude{
		Paths: pick(states[0], []string{"^legacy/"}, []string{"^modern/"}),
		Rules: pick(states[1], []string{"staticcheck:SA1019"}, []string{"staticcheck:SA1000"}),
	}
	if text := pick(states[2], []string{"deprecated"}, []string{"^removed"}); text != nil {
		e.Text = text[0]
	}
	switch states[3] {
	case unset:
	case matching:
		e.Tests = new(isTest)
	case mismatching:
		e.Tests = new(!isTest)
	}
	return e
}

func TestExcludeMatchesIffEverySetFieldMatches(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"legacy/a.go", "legacy/a_test.go"} {
		isTest := path == "legacy/a_test.go"
		for n := range 81 {
			states := [4]fieldState{fieldState(n % 3), fieldState(n / 3 % 3), fieldState(n / 9 % 3), fieldState(n / 27 % 3)}
			t.Run(fmt.Sprintf("%s/paths=%d,rules=%d,text=%d,tests=%d", path, states[0], states[1], states[2], states[3]), func(t *testing.T) {
				t.Parallel()
				checkExclude(t, excludeFor(states, isTest), path, states)
			})
		}
	}
}

func checkExclude(t *testing.T, e config.Exclude, path string, states [4]fieldState) {
	t.Helper()
	compiled, err := config.CompileExcludes([]config.Exclude{e})
	if states == [4]fieldState{} {
		if want := "exclude[0]: set at least one of paths, rules, tests, text"; err == nil || err.Error() != want {
			t.Fatalf("CompileExcludes error = %v, want %q", err, want)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	want := states[0] != mismatching && states[1] != mismatching && states[2] != mismatching && states[3] != mismatching
	if got := compiled[0].Matches(path, "staticcheck", "SA1019", "x is deprecated"); got != want {
		t.Fatalf("Matches(%q) with %+v = %v, want %v", path, e, got, want)
	}
}

func TestExcludeListFieldMatchesWhenAnyEntryMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		exclude config.Exclude
		path    string
		want    bool
	}{
		{"second path matches", config.Exclude{Paths: []string{"^modern/", "^legacy/"}}, "legacy/a.go", true},
		{"no path matches", config.Exclude{Paths: []string{"^modern/", "^vendor/"}}, "legacy/a.go", false},
		{"path regexp is unanchored", config.Exclude{Paths: []string{"legacy/"}}, "internal/legacy/a.go", true},
		{"second rule matches", config.Exclude{Rules: []string{"unused", "staticcheck:SA1019"}}, "a.go", true},
		{"no rule matches", config.Exclude{Rules: []string{"unused", "staticcheck:SA1000"}}, "a.go", false},
		{"rule name covers its checks", config.Exclude{Rules: []string{"staticcheck"}}, "a.go", true},
		{"text regexp is unanchored", config.Exclude{Text: "is deprec"}, "a.go", true},
	}
	for _, c := range cases {
		compiled, err := config.CompileExcludes([]config.Exclude{c.exclude})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := compiled[0].Matches(c.path, "staticcheck", "SA1019", "x is deprecated"); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestExcludeTestsFieldSelectsByTestGoSuffix(t *testing.T) {
	t.Parallel()
	compiled, err := config.CompileExcludes([]config.Exclude{{Tests: new(true)}, {Tests: new(false)}})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{
		"a_test.go":          true,
		"pkg/a_test.go":      true,
		"pkg/test.go":        false,
		"pkg/a.go":           false,
		"pkg/a_test.go.orig": false,
		"pkg/a_test.gox":     false,
		"a_test.go/b.go":     false,
	}
	for path, isTest := range paths {
		if got := compiled[0].Matches(path, "printf", "", "m"); got != isTest {
			t.Errorf("tests=true Matches(%q) = %v, want %v", path, got, isTest)
		}
		if got := compiled[1].Matches(path, "printf", "", "m"); got != !isTest {
			t.Errorf("tests=false Matches(%q) = %v, want %v", path, got, !isTest)
		}
	}
}

type findingID struct{ rule, check string }

func TestMatchTokenRuleCoversAllChecksAndRuleCheckOnlyOne(t *testing.T) {
	t.Parallel()
	findings := []findingID{{"staticcheck", "SA1019"}, {"staticcheck", "SA1000"}, {"unused", ""}, {"printf", ""}}
	matches := map[string][]findingID{
		"staticcheck":          {{"staticcheck", "SA1019"}, {"staticcheck", "SA1000"}},
		"staticcheck:SA1019":   {{"staticcheck", "SA1019"}},
		"unused":               {{"unused", ""}},
		"printf":               {{"printf", ""}},
		"staticcheck:":         nil,
		"unused:":              nil,
		"unused:U1000":         nil,
		"SA1019":               nil,
		":SA1019":              nil,
		"staticcheck:SA10":     nil,
		"staticcheck:SA1019:x": nil,
		"Staticcheck":          nil,
		"static":               nil,
		"":                     nil,
	}
	for token, want := range matches {
		for _, f := range findings {
			if got, wanted := config.MatchToken(token, f.rule, f.check), slices.Contains(want, f); got != wanted {
				t.Errorf("MatchToken(%q, %q, %q) = %v, want %v", token, f.rule, f.check, got, wanted)
			}
		}
	}
}
