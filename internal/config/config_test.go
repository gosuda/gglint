package config_test

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

var modeNames = [...]string{config.Basic: "basic", config.Standard: "standard", config.Strict: "strict"}

var allModes = []config.Mode{config.Basic, config.Standard, config.Strict}

var invalidModeNames = []string{"", "loose", "Basic", "STRICT", " standard", "strict ", "std", "basic,strict"}

var limitFields = map[string]func(*config.Complexity) *int{
	"max_cognitive":         func(c *config.Complexity) *int { return &c.MaxCognitive },
	"max_cyclomatic":        func(c *config.Complexity) *int { return &c.MaxCyclomatic },
	"max_nesting":           func(c *config.Complexity) *int { return &c.MaxNesting },
	"max_lines":             func(c *config.Complexity) *int { return &c.MaxLines },
	"max_params":            func(c *config.Complexity) *int { return &c.MaxParams },
	"max_bool_params":       func(c *config.Complexity) *int { return &c.MaxBoolParams },
	"max_interface_methods": func(c *config.Complexity) *int { return &c.MaxInterfaceMethods },
	"max_file_lines":        func(c *config.Complexity) *int { return &c.MaxFileLines },
}

var readmeLimits = map[string][3]int{
	"max_cognitive":         {24, 19, 14},
	"max_cyclomatic":        {30, 15, 9},
	"max_nesting":           {6, 5, 3},
	"max_lines":             {150, 80, 60},
	"max_params":            {8, 6, 4},
	"max_bool_params":       {0, 2, 1},
	"max_interface_methods": {0, 10, 5},
	"max_file_lines":        {1499, 999, 799},
}

func readmeComplexity(m config.Mode) config.Complexity {
	var c config.Complexity
	for key, field := range limitFields {
		*field(&c) = readmeLimits[key][m]
	}
	return c
}

func modeByName(name string) config.Mode {
	return config.Mode(slices.Index(modeNames[:], name))
}

func unknownMode(name string) string {
	return fmt.Sprintf("unknown mode %q (want basic, standard, or strict)", name)
}

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

func regexpError(t *testing.T, source string) string {
	t.Helper()
	_, err := regexp.Compile(source)
	if err == nil {
		t.Fatalf("regexp %q compiles", source)
	}
	return err.Error()
}

func TestLoadRejectsInvalidFileWithDocumentedPathPrefixedMessage(t *testing.T) {
	type errorCase struct{ name, toml, want string }
	cases := []errorCase{
		{"unknown top key", "mode = \"basic\"\nbogus = 1\n", "2:1: unknown key bogus"},
		{"unknown indented run key", "[run]\n  test = true\n", "2:3: unknown key run.test"},
		{"unknown complexity key", "\n[rules.complexity]\nmax_bogus = 1\n", "3:1: unknown key rules.complexity.max_bogus"},
		{"empty deny path", "[rules.checkimports]\ndeny = [{ path = \"\" }]\n", "rules.checkimports.deny[0]: path is empty"},
		{"second deny lacks path", "[rules.checkimports]\ndeny = [{ path = \"archive/zip\" }, { allow_from = [\"m/x\"] }]\n", "rules.checkimports.deny[1]: path is empty"},
		{"exclude without fields", "[[exclude]]\n", "exclude[0]: set at least one of paths, rules, tests, text"},
		{"exclude with only empty fields", "[[exclude]]\npaths = []\nrules = []\ntext = \"\"\n", "exclude[0]: set at least one of paths, rules, tests, text"},
		{"second exclude without fields", "[[exclude]]\ntext = \"x\"\n[[exclude]]\n", "exclude[1]: set at least one of paths, rules, tests, text"},
		{"bad second path regexp", "[[exclude]]\ntext = \"x\"\n[[exclude]]\npaths = [\"ok\", \"(\"]\n", "exclude[1].paths[1]: " + regexpError(t, "(")},
		{"bad text regexp", "[[exclude]]\ntext = \"[\"\n", "exclude[0].text: " + regexpError(t, "[")},
	}
	for _, name := range invalidModeNames {
		cases = append(cases, errorCase{"mode " + name, fmt.Sprintf("mode = %q\n", name), unknownMode(name)})
	}
	for _, tc := range cases {
		for _, form := range []string{"absolute", "relative"} {
			t.Run(tc.name+"/"+form, func(t *testing.T) { checkFileError(t, tc.toml, form, tc.want) })
		}
	}
}

func checkFileError(t *testing.T, toml, form, message string) {
	t.Helper()
	dir := t.TempDir()
	path := writeFile(t, dir, config.FileName, toml)
	arg := map[string]string{"absolute": path, "relative": config.FileName}[form]
	cfg, err := config.Load(arg, dir, "")
	if want := path + ": " + message; err == nil || err.Error() != want {
		t.Fatalf("Load(%q) error = %v, want %q (config %+v)", arg, err, want, cfg)
	}
}

func TestLoadRejectsUnreadableOrMistypedFileWithPathPrefix(t *testing.T) {
	cases := []struct {
		name, toml string
		write      bool
	}{
		{"missing file", "", false},
		{"mode not a string", "mode = 1\n", true},
		{"tests not a bool", "[run]\ntests = \"yes\"\n", true},
		{"unterminated string", "mode = \"basic\n", true},
		{"limit not an int", "[rules.complexity]\nmax_lines = \"80\"\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.FileName)
			if tc.write {
				writeFile(t, dir, config.FileName, tc.toml)
			}
			_, err := config.Load(path, dir, "")
			if err == nil || !strings.HasPrefix(err.Error(), path+": ") || len(err.Error()) == len(path)+2 {
				t.Fatalf("Load error = %v, want %q prefix and a reason", err, path+": ")
			}
		})
	}
	t.Run("directory as config", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := config.Load(dir, dir, ""); err == nil || !strings.HasPrefix(err.Error(), dir+": ") {
			t.Fatalf("Load error = %v, want %q prefix", err, dir+": ")
		}
	})
}

func TestLoadRejectsInvalidModeOverrideWithoutPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, config.FileName, "mode = \"strict\"\n")
	for _, override := range invalidModeNames[1:] {
		for _, arg := range []string{"", path} {
			if _, err := config.Load(arg, dir, override); err == nil || err.Error() != unknownMode(override) {
				t.Errorf("Load(%q, -mode %q) error = %v, want %q", arg, override, err, unknownMode(override))
			}
		}
	}
}

func TestLoadModeOverrideBeatsFileModeAndLimitsFollowFinalMode(t *testing.T) {
	names := []string{"", "basic", "standard", "strict"}
	for _, fileMode := range names {
		for _, override := range names {
			for _, limits := range []string{"", "[rules.complexity]\nmax_nesting = 2\n"} {
				checkModePrecedence(t, fileMode, override, limits)
			}
		}
	}
}

func checkModePrecedence(t *testing.T, fileMode, override, limits string) {
	t.Helper()
	dir := t.TempDir()
	content := limits
	if fileMode != "" {
		content = fmt.Sprintf("mode = %q\n", fileMode) + limits
	}
	path := writeFile(t, dir, config.FileName, content)
	cfg, err := config.Load(path, dir, override)
	if err != nil {
		t.Fatal(err)
	}
	want := modeByName(cmp.Or(override, fileMode, "standard"))
	wantLimits := readmeComplexity(want)
	if limits != "" {
		wantLimits.MaxNesting = 2
	}
	if cfg.Mode != want || cfg.Complexity != wantLimits {
		t.Errorf("file %q, -mode %q, limits %q: mode %v complexity %+v, want %v %+v", fileMode, override, limits, cfg.Mode, cfg.Complexity, want, wantLimits)
	}
}

func TestLoadWithoutFileHonorsModeOverride(t *testing.T) {
	for _, override := range []string{"basic", "standard", "strict"} {
		t.Run(fmt.Sprintf("%q", override), func(t *testing.T) {
			dir := t.TempDir()
			cfg, err := config.Load("", dir, override)
			if err != nil {
				t.Fatal(err)
			}
			want := modeByName(override)
			if cfg.Mode != want || cfg.Complexity != readmeComplexity(want) || cfg.Path != "" {
				t.Fatalf("config %+v, want mode %v with its limits and no path", cfg, want)
			}
		})
	}
}

func TestLoadComplexityOverrideSetsOneLimitKeepsOthersAndRejectsNegative(t *testing.T) {
	for _, m := range allModes {
		for key := range limitFields {
			for _, value := range []int{-1, 0, 1, readmeLimits[key][m] + 1} {
				checkLimitOverride(t, m, key, value)
			}
		}
	}
}

func checkLimitOverride(t *testing.T, m config.Mode, key string, value int) {
	t.Helper()
	dir := t.TempDir()
	path := writeFile(t, dir, config.FileName, fmt.Sprintf("mode = %q\n[rules.complexity]\n%s = %d\n", modeNames[m], key, value))
	cfg, err := config.Load(path, dir, "")
	if value < 0 {
		if want := fmt.Sprintf("%s: rules.complexity.%s: must be >= 0", path, key); err == nil || err.Error() != want {
			t.Errorf("%v %s=%d: Load error = %v, want %q", m, key, value, err, want)
		}
		return
	}
	want := readmeComplexity(m)
	*limitFields[key](&want) = value
	if err != nil || cfg.Complexity != want {
		t.Errorf("%v %s=%d: complexity = %+v, %v; want %+v", m, key, value, cfg.Complexity, err, want)
	}
}

func TestLoadCarriesEveryFileFieldAndRootsExcludesAtConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, filepath.Join("conf", config.FileName), `
mode = "strict"
enable = ["dupl", "goconst"]
disable = ["noinit"]

[run]
tests = false
build_tags = ["e2e", "integration"]

[rules.complexity]
max_cognitive = 20
max_lines = 0

[rules.checkimports]
deny = [{ path = "archive/zip", allow_from = ["example.com/m/internal/zipx"] }, { path = "unsafe/..." }]

[[exclude]]
paths = ["^legacy/"]
rules = ["complexity", "staticcheck:SA1019"]
text = "too complex"

[[exclude]]
tests = true
rules = ["goconst"]

[[exclude]]
tests = false
text = "x"
`)
	limits := readmeComplexity(config.Strict)
	limits.MaxCognitive, limits.MaxLines = 20, 0
	want := config.Config{
		Path:        path,
		ExcludeBase: filepath.Dir(path),
		Mode:        config.Strict,
		Enable:      []string{"dupl", "goconst"},
		Disable:     []string{"noinit"},
		BuildTags:   []string{"e2e", "integration"},
		Complexity:  limits,
		Deny: []config.Deny{
			{Path: "archive/zip", AllowFrom: []string{"example.com/m/internal/zipx"}},
			{Path: "unsafe/..."},
		},
		Excludes: []config.Exclude{
			{Paths: []string{"^legacy/"}, Rules: []string{"complexity", "staticcheck:SA1019"}, Text: "too complex"},
			{Tests: new(true), Rules: []string{"goconst"}},
			{Tests: new(false), Text: "x"},
		},
	}
	for _, arg := range []string{path, filepath.Join("conf", config.FileName)} {
		cfg, err := config.Load(arg, dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg, want) {
			t.Fatalf("Load(%q) =\n%+v\nwant\n%+v", arg, cfg, want)
		}
	}
}

func TestLoadWithoutFileUsesDefaultsRootedAtAbsoluteWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	want := config.Config{ExcludeBase: dir, Mode: config.Standard, Tests: true, Complexity: readmeComplexity(config.Standard)}
	t.Chdir(dir)
	for _, wd := range []string{dir, "."} {
		cfg, err := config.Load("", wd, "")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg, want) {
			t.Fatalf("Load(\"\", %q) = %+v, want %+v", wd, cfg, want)
		}
	}
}

func TestLoadEmptyFileEqualsDefaultsWithPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, filepath.Join("sub", config.FileName), "")
	cfg, err := config.Load(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{Path: path, ExcludeBase: filepath.Dir(path), Mode: config.Standard, Tests: true, Complexity: readmeComplexity(config.Standard)}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("config = %+v, want %+v", cfg, want)
	}
}

func TestLoadRunTestsDefaultsToTrue(t *testing.T) {
	cases := []struct {
		toml string
		want bool
	}{
		{"", true},
		{"[run]\n", true},
		{"[run]\ntests = true\n", true},
		{"[run]\ntests = false\n", false},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		cfg, err := config.Load(writeFile(t, dir, config.FileName, tc.toml), dir, "")
		if err != nil || cfg.Tests != tc.want {
			t.Errorf("%q: Tests = %v, %v; want %v", tc.toml, cfg.Tests, err, tc.want)
		}
	}
}

func TestLoadAcceptsExcludeWithExactlyOneFieldSet(t *testing.T) {
	cases := []struct {
		toml string
		want config.Exclude
	}{
		{"paths = [\"^a/\"]", config.Exclude{Paths: []string{"^a/"}}},
		{"rules = [\"printf\"]", config.Exclude{Rules: []string{"printf"}}},
		{"text = \"x\"", config.Exclude{Text: "x"}},
		{"tests = true", config.Exclude{Tests: new(true)}},
		{"tests = false", config.Exclude{Tests: new(false)}},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		cfg, err := config.Load(writeFile(t, dir, config.FileName, "[[exclude]]\n"+tc.toml+"\n"), dir, "")
		if err != nil {
			t.Errorf("%s: %v", tc.toml, err)
			continue
		}
		if want := []config.Exclude{tc.want}; !reflect.DeepEqual(cfg.Excludes, want) {
			t.Errorf("%s: excludes = %+v, want %+v", tc.toml, cfg.Excludes, want)
		}
	}
}
