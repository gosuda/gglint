package config_test

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"testing/quick"

	"github.com/gosuda/gglint/internal/config"
)

func populated(m config.Mode) config.Config {
	return config.Config{
		Path:        "/work/conf/gglint.toml",
		ExcludeBase: "/work/conf",
		Mode:        m,
		Enable:      []string{"goconst", "dupl", "chanblock"},
		Disable:     []string{"noinit", "closer"},
		Tests:       true,
		BuildTags:   []string{"integration", "e2e"},
		Complexity:  config.Complexity{MaxCognitive: 1, MaxCyclomatic: 2, MaxNesting: 3, MaxLines: 4, MaxParams: 5, MaxBoolParams: 6, MaxInterfaceMethods: 7, MaxFileLines: 8},
		Deny: []config.Deny{
			{Path: "archive/zip", AllowFrom: []string{"example.com/m/internal/zipx/..."}},
			{Path: "unsafe", AllowFrom: []string{"example.com/m/internal/raw"}},
		},
		Excludes: []config.Exclude{
			{Paths: []string{"^legacy/"}, Rules: []string{"complexity"}, Text: "too complex", Tests: new(true)},
			{Paths: []string{"^gen/"}, Rules: []string{"staticcheck:SA1019"}, Text: "deprecated", Tests: new(false)},
			{Paths: []string{"^vendor/"}, Rules: []string{"goconst"}, Text: "occurrences"},
		},
	}
}

func TestDecodeInvertsEncodeUpToSortedSlices(t *testing.T) {
	for _, m := range allModes {
		want := populated(m)
		want.Enable = []string{"chanblock", "dupl", "goconst"}
		want.Disable = []string{"closer", "noinit"}
		want.BuildTags = []string{"e2e", "integration"}
		data := populated(m).Encode()
		got, err := config.Decode(data)
		if err != nil {
			t.Fatalf("Decode(%s): %v", data, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Decode(Encode(c)) =\n%+v\nwant\n%+v", got, want)
		}
		if again := got.Encode(); !bytes.Equal(again, data) {
			t.Fatalf("Encode is not a fixpoint across Decode:\n%s\n%s", data, again)
		}
	}
}

func shuffled(r *rand.Rand, s []string) []string {
	s = slices.Clone(s)
	r.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
	return s
}

func TestEncodeIsInvariantUnderSliceOrder(t *testing.T) {
	property := func(enable, disable, tags []string, seed uint64) bool {
		r := rand.New(rand.NewPCG(seed, seed))
		c := config.Config{Mode: config.Strict, Enable: enable, Disable: disable, BuildTags: tags}
		d := c
		d.Enable, d.Disable, d.BuildTags = shuffled(r, enable), shuffled(r, disable), shuffled(r, tags)
		return bytes.Equal(c.Encode(), d.Encode())
	}
	if err := quick.Check(property, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejectsUnknownMembersInvalidValuesAndBadExcludes(t *testing.T) {
	if _, err := config.Decode([]byte(`{"mode":"strict","excludes":[{"text":"x"}]}`)); err != nil {
		t.Fatalf("Decode rejected a valid document: %v", err)
	}
	bad := map[string]string{
		"unknown top member":        `{"mode":"basic","bogus":1}`,
		"unknown complexity member": `{"mode":"basic","complexity":{"max_bogus":1}}`,
		"unknown deny member":       `{"mode":"basic","deny":[{"path":"x","bogus":1}]}`,
		"unknown exclude member":    `{"mode":"basic","excludes":[{"text":"x","bogus":1}]}`,
		"unknown mode":              `{"mode":"loose"}`,
		"bad path regexp":           `{"mode":"basic","excludes":[{"paths":["("]}]}`,
		"bad text regexp":           `{"mode":"basic","excludes":[{"text":"["}]}`,
		"exclude without fields":    `{"mode":"basic","excludes":[{}]}`,
		"mistyped tests":            `{"mode":"basic","tests":"yes"}`,
		"truncated":                 `{"mode":"basic"`,
		"empty":                     ``,
	}
	for name, doc := range bad {
		if c, err := config.Decode([]byte(doc)); err == nil {
			t.Errorf("%s: Decode(%s) = %+v, want error", name, doc, c)
		}
	}
}
