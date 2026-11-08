package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/gosuda/gglint/internal/config"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{"unknown mode", `mode = "loose"`, `unknown mode "loose" (want basic, standard, or strict)`},
		{"unknown key", "mode = \"basic\"\nbogus = 1\n", `2:1: unknown key bogus`},
		{"unknown nested key", "[rules.complexity]\nmax_bogus = 1\n", `2:1: unknown key rules.complexity.max_bogus`},
		{"negative limit", "[rules.complexity]\nmax_lines = -1\n", `rules.complexity.max_lines: must be >= 0`},
		{"empty deny path", "[rules.checkimports]\ndeny = [{ path = \"\" }]\n", `rules.checkimports.deny[0]: path is empty`},
		{"empty exclude", "[[exclude]]\n", `exclude[0]: set at least one of paths, rules, tests, text`},
		{"bad path regexp", "[[exclude]]\npaths = [\"ok\", \"(\"]\n", "exclude[0].paths[1]: error parsing regexp: missing closing ): `(`"},
		{"bad text regexp", "[[exclude]]\ntext = \"[\"\n", "exclude[0].text: error parsing regexp: missing closing ]: `[`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, config.FileName, tt.toml)
			_, err := config.Load(path, dir, "")
			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if want := path + ": " + tt.want; err.Error() != want {
				t.Fatalf("Load error = %q, want %q", err, want)
			}
		})
	}
}

func TestLoadModeOverrideError(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, config.FileName, `mode = "strict"`)
	_, err := config.Load(path, dir, "loud")
	if want := `unknown mode "loud" (want basic, standard, or strict)`; err == nil || err.Error() != want {
		t.Fatalf("Load error = %v, want %q", err, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load("", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{
		ExcludeBase: dir,
		Mode:        config.Standard,
		Tests:       true,
		Complexity:  config.ComplexityFor(config.Standard),
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestLoadFullFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "conf/"+config.FileName, `
mode = "strict"
enable = ["goconst"]
disable = ["noinit"]

[run]
tests = false
build_tags = ["integration"]

[rules.complexity]
max_cognitive = 20
max_lines = 0

[rules.checkimports]
deny = [{ path = "archive/zip", allow_from = ["example.com/m/internal/zipx"] }]

[[exclude]]
paths = ["^legacy/"]
rules = ["complexity", "staticcheck:SA1019"]
text = "too complex"
`)
	cfg, err := config.Load(path, dir, "basic")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != config.Basic || cfg.Tests || cfg.Path != path || cfg.ExcludeBase != filepath.Dir(path) {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	wantComplexity := config.ComplexityFor(config.Basic)
	wantComplexity.MaxCognitive = 20
	wantComplexity.MaxLines = 0
	if cfg.Complexity != wantComplexity {
		t.Fatalf("complexity = %+v, want %+v", cfg.Complexity, wantComplexity)
	}
	if len(cfg.Deny) != 1 || cfg.Deny[0].Path != "archive/zip" || len(cfg.Excludes) != 1 {
		t.Fatalf("deny/excludes not loaded: %+v", cfg)
	}
}

func TestLoadExcludeTests(t *testing.T) {
	tests := []struct {
		name  string
		toml  string
		want  *bool
		rules []string
	}{
		{"tests with rules", "[[exclude]]\ntests = true\nrules = [\"goconst\"]\n", new(true), []string{"goconst"}},
		{"tests alone", "[[exclude]]\ntests = true\n", new(true), nil},
		{"tests false", "[[exclude]]\ntests = false\n", new(false), nil},
		{"tests absent", "[[exclude]]\nrules = [\"goconst\"]\n", nil, []string{"goconst"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, config.FileName, tt.toml)
			cfg, err := config.Load(path, dir, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Excludes) != 1 {
				t.Fatalf("excludes = %+v, want one entry", cfg.Excludes)
			}
			got := cfg.Excludes[0]
			if !reflect.DeepEqual(got.Tests, tt.want) || !reflect.DeepEqual(got.Rules, tt.rules) {
				t.Fatalf("exclude = %+v, want tests %v rules %v", got, tt.want, tt.rules)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, config.FileName, "mode = \"strict\"\nenable = [\"b\", \"a\"]\n[[exclude]]\ntext = \"x\"\n")
	cfg, err := config.Load(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	data := cfg.Encode()
	back, err := config.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Encode(), data) {
		t.Fatalf("Encode not stable across Decode:\n%s\n%s", data, back.Encode())
	}
	if back.Mode != config.Strict || !reflect.DeepEqual(back.Enable, []string{"a", "b"}) {
		t.Fatalf("decoded = %+v", back)
	}
}

func TestEncodeDecodeKeepsExcludeTests(t *testing.T) {
	cfg := config.Config{Excludes: []config.Exclude{
		{Text: "x"},
		{Tests: new(false)},
		{Tests: new(true), Rules: []string{"goconst"}},
	}}
	back, err := config.Decode(cfg.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Excludes) != len(cfg.Excludes) {
		t.Fatalf("excludes = %+v, want %d entries", back.Excludes, len(cfg.Excludes))
	}
	for i, want := range cfg.Excludes {
		if got := back.Excludes[i].Tests; !reflect.DeepEqual(got, want.Tests) {
			t.Errorf("excludes[%d].Tests = %v, want %v", i, got, want.Tests)
		}
	}
}

func TestEncodeIgnoresSliceOrder(t *testing.T) {
	a := config.Config{Enable: []string{"x", "y"}}
	b := config.Config{Enable: []string{"y", "x"}}
	if !bytes.Equal(a.Encode(), b.Encode()) {
		t.Fatal("Encode depends on Enable order")
	}
}

func TestDecodeRejectsBadExclude(t *testing.T) {
	bad := config.Config{Excludes: []config.Exclude{{Text: "("}}}
	if _, err := config.Decode(bad.Encode()); err == nil {
		t.Fatal("Decode accepted an invalid regexp")
	}
}

func TestDiscoverStopsAtGitBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, config.FileName, `mode = "basic"`)
	repo := filepath.Join(root, "repo")
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := config.Discover(nested)
	if err != nil || got != "" {
		t.Fatalf("Discover beyond .git = %q, %v; want none", got, err)
	}
	inRepo := writeFile(t, repo, config.FileName, `mode = "strict"`)
	got, err = config.Discover(nested)
	if err != nil || got != inRepo {
		t.Fatalf("Discover = %q, %v; want %q", got, err, inRepo)
	}
}

func TestDiscoverBoundaryDirIsSearched(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeFile(t, repo, config.FileName, `mode = "basic"`)
	got, err := config.Discover(repo)
	if err != nil || got != want {
		t.Fatalf("Discover = %q, %v; want %q", got, err, want)
	}
}

func TestDiscoverWithoutGitWalksToRoot(t *testing.T) {
	root := t.TempDir()
	want := writeFile(t, root, config.FileName, `mode = "basic"`)
	nested := filepath.Join(root, "x", "y")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := config.Discover(nested)
	if err != nil || got != want {
		t.Fatalf("Discover = %q, %v; want %q", got, err, want)
	}
}

func limitsOf(c config.Complexity) []int {
	return []int{
		c.MaxCognitive, c.MaxCyclomatic, c.MaxNesting, c.MaxLines, c.MaxParams,
		c.MaxBoolParams, c.MaxInterfaceMethods, c.MaxFileLines,
	}
}

func tighter(strict, loose int) bool {
	if loose == 0 {
		return true
	}
	return strict != 0 && strict <= loose
}

func TestComplexityMonotoneAcrossModes(t *testing.T) {
	property := func(metric uint8, pair uint8) bool {
		lower, upper := config.Mode(pair%2), config.Mode(pair%2+1)
		i := int(metric) % len(limitsOf(config.Complexity{}))
		return tighter(limitsOf(config.ComplexityFor(upper))[i], limitsOf(config.ComplexityFor(lower))[i])
	}
	if err := quick.Check(property, nil); err != nil {
		t.Fatal(err)
	}
}

func TestModeTextRoundTrip(t *testing.T) {
	for _, m := range []config.Mode{config.Basic, config.Standard, config.Strict} {
		text, err := m.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back config.Mode
		if err := back.UnmarshalText(text); err != nil || back != m || m.String() != string(text) {
			t.Fatalf("round trip of %v = %v, %v", m, back, err)
		}
	}
}

func TestCompiledExcludeMatching(t *testing.T) {
	compiled, err := config.CompileExcludes([]config.Exclude{
		{Paths: []string{"^legacy/"}, Rules: []string{"staticcheck:SA1019"}, Text: "deprecated"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := compiled[0]
	cases := []struct {
		path, rule, check, message string
		want                       bool
	}{
		{"legacy/a.go", "staticcheck", "SA1019", "x is deprecated", true},
		{"legacy/a.go", "staticcheck", "SA1000", "x is deprecated", false},
		{"modern/a.go", "staticcheck", "SA1019", "x is deprecated", false},
		{"legacy/a.go", "staticcheck", "SA1019", "other", false},
	}
	for _, c := range cases {
		if got := ex.Matches(c.path, c.rule, c.check, c.message); got != c.want {
			t.Errorf("Matches(%q, %q, %q, %q) = %v, want %v", c.path, c.rule, c.check, c.message, got, c.want)
		}
	}
	if !config.MatchToken("staticcheck", "staticcheck", "SA1019") {
		t.Error("a rule name must cover every check of the rule")
	}
}

func TestCompiledExcludeTestsMatching(t *testing.T) {
	cases := []struct {
		name       string
		exclude    config.Exclude
		path, rule string
		want       bool
	}{
		{"true matches test file", config.Exclude{Tests: new(true)}, "a/x_test.go", "printf", true},
		{"true skips other file", config.Exclude{Tests: new(true)}, "a/x.go", "printf", false},
		{"false skips test file", config.Exclude{Tests: new(false)}, "a/x_test.go", "printf", false},
		{"false matches other file", config.Exclude{Tests: new(false)}, "a/x.go", "printf", true},
		{"absent matches test file", config.Exclude{Rules: []string{"printf"}}, "a/x_test.go", "printf", true},
		{"absent matches other file", config.Exclude{Rules: []string{"printf"}}, "a/x.go", "printf", true},
		{"true with rules matches rule", config.Exclude{Tests: new(true), Rules: []string{"goconst"}}, "a/x_test.go", "goconst", true},
		{"true with rules skips other rule", config.Exclude{Tests: new(true), Rules: []string{"goconst"}}, "a/x_test.go", "printf", false},
		{"true with rules skips other file", config.Exclude{Tests: new(true), Rules: []string{"goconst"}}, "a/x.go", "goconst", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			compiled, err := config.CompileExcludes([]config.Exclude{c.exclude})
			if err != nil {
				t.Fatal(err)
			}
			if got := compiled[0].Matches(c.path, c.rule, "", "message"); got != c.want {
				t.Fatalf("Matches(%q, %q) = %v, want %v", c.path, c.rule, got, c.want)
			}
		})
	}
}
