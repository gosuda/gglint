package cli_test

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

var (
	binary  string
	goCache string
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "gglint-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	if err := setup(context.Background(), dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

func setup(ctx context.Context, dir string) error {
	binary = filepath.Join(dir, "gglint")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "github.com/gosuda/gglint/cmd/gglint")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}
	out, err := exec.CommandContext(ctx, "go", "env", "GOCACHE").Output()
	if err != nil {
		return fmt.Errorf("go env GOCACHE: %w", err)
	}
	goCache = strings.TrimSpace(string(out))
	return nil
}

type result struct {
	code   int
	stdout string
	stderr string
}

func childEnv(t *testing.T, dir string) []string {
	t.Helper()
	return append(os.Environ(),
		"PWD="+dir,
		"XDG_CACHE_HOME="+t.TempDir(),
		"GOCACHE="+goCache,
		"GOFLAGS=-mod=mod",
		"GOWORK=off",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
}

func gglint(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...)
	cmd.Dir = dir
	cmd.Env = childEnv(t, dir)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("gglint %v: %v", args, err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = childEnv(t, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func workspace(t *testing.T, toml string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, os.DirFS("testdata/modes")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if toml != "" {
		writeFile(t, filepath.Join(dir, config.FileName), toml)
	}
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func position(t *testing.T, dir, rel, needle string) (line, column int) {
	t.Helper()
	n := 0
	for text := range strings.Lines(readFile(t, filepath.Join(dir, rel))) {
		n++
		if i := strings.Index(text, needle); i >= 0 {
			return n, i + 1
		}
	}
	t.Fatalf("%s: no line contains %q", rel, needle)
	return 0, 0
}

func appendToLine(t *testing.T, dir, rel, needle, suffix string) {
	t.Helper()
	line, _ := position(t, dir, rel, needle)
	path := filepath.Join(dir, rel)
	lines := strings.Split(readFile(t, path), "\n")
	lines[line-1] += suffix
	writeFile(t, path, strings.Join(lines, "\n"))
}

func withNolint(rel, needle string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		appendToLine(t, dir, rel, needle, " //nolint:printf // reason")
	}
}

func assertResult(t *testing.T, got, want result) {
	t.Helper()
	if got != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr %q",
			got.code, got.stdout, got.stderr, want.code, want.stdout, want.stderr)
	}
}

type expect struct {
	file    string
	needle  string
	message string
	token   string
}

func (e expect) text(t *testing.T, dir string) string {
	t.Helper()
	line, column := position(t, dir, e.file, e.needle)
	return fmt.Sprintf("%s:%d:%d: %s [%s]\n", e.file, line, column, e.message, e.token)
}

func printfMessage(pkg, arg string) string {
	return fmt.Sprintf("example.com/modes/%s.logf format %%d has arg %q of wrong type string", pkg, arg)
}

func printfVerb(arg string) string {
	return `%d\n", "` + arg + `")`
}

func summary(toml string, findings int) string {
	var b strings.Builder
	if toml != "" {
		b.WriteString("gglint: config gglint.toml\n")
	}
	if findings > 0 {
		fmt.Fprintf(&b, "gglint: %d findings (mode standard)\n", findings)
	}
	return b.String()
}

func TestRunText(t *testing.T) {
	t.Parallel()
	aFinding := expect{file: "a/a.go", needle: printfVerb("x"), message: printfMessage("a", "x"), token: "printf"}
	cFinding := expect{file: "c/c.go", needle: printfVerb("y"), message: printfMessage("c", "y"), token: "printf"}
	unused := expect{file: "b/b.go", needle: "//nolint", message: "unused nolint directive for printf", token: rules.NolintName}
	tFinding := expect{file: "t/t.go", needle: printfVerb("x"), message: printfMessage("t", "x"), token: "printf"}
	tTestFinding := expect{file: "t/t_test.go", needle: printfVerb("y"), message: printfMessage("t", "y"), token: "printf"}
	cases := []struct {
		name string
		toml string
		edit func(*testing.T, string)
		args []string
		want []expect
	}{
		{name: "printf finding", args: []string{"run", "./a"}, want: []expect{aFinding}},
		{name: "clean package", args: []string{"run", "./b"}},
		{name: "two packages", args: []string{"run", "./a", "./c"}, want: []expect{aFinding, cFinding}},
		{name: "exclude path", toml: "[[exclude]]\npaths = [\"^c/\"]\n", args: []string{"run", "./a", "./c"}, want: []expect{aFinding}},
		{name: "nolint suppresses", edit: withNolint("a/a.go", `logf("`), args: []string{"run", "./a"}},
		{name: "unused nolint", edit: withNolint("b/b.go", "fmt.Printf"), args: []string{"run", "./b"}, want: []expect{unused}},
		{name: "test file finding", args: []string{"run", "./t"}, want: []expect{tFinding, tTestFinding}},
		{name: "exclude tests", toml: "[[exclude]]\ntests = true\nrules = [\"printf\"]\n", args: []string{"run", "./t"}, want: []expect{tFinding}},
		{name: "exclude non-tests", toml: "[[exclude]]\ntests = false\nrules = [\"printf\"]\n", args: []string{"run", "./t"}, want: []expect{tTestFinding}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := workspace(t, tc.toml)
			if tc.edit != nil {
				tc.edit(t, dir)
			}
			var stdout strings.Builder
			for _, e := range tc.want {
				stdout.WriteString(e.text(t, dir))
			}
			code := 0
			if len(tc.want) > 0 {
				code = 1
			}
			assertResult(t, gglint(t, dir, tc.args...), result{code: code, stdout: stdout.String(), stderr: summary(tc.toml, len(tc.want))})
		})
	}
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		toml   string
		args   []string
		stderr string
	}{
		{name: "unknown key", toml: "bogus = 1\n", stderr: `^gglint: CONFIG: \d+:\d+: unknown key bogus\n$`},
		{name: "unknown mode", toml: "mode = \"loose\"\n", stderr: `^gglint: CONFIG: unknown mode "loose" \(want basic, standard, or strict\)\n$`},
		{name: "unknown enable", toml: "enable = [\"nosuch\"]\n", stderr: `^gglint: CONFIG: enable: unknown rule "nosuch"\n$`},
		{name: "unknown disable", toml: "disable = [\"nosuch\"]\n", stderr: `^gglint: CONFIG: disable: unknown rule "nosuch"\n$`},
		{name: "enable and disable", toml: "enable = [\"chanblock\"]\ndisable = [\"chanblock\"]\n", stderr: `^gglint: CONFIG: rule "chanblock" is in both enable and disable\n$`},
		{name: "disable typecheck", toml: "disable = [\"typecheck\"]\n", stderr: `^gglint: CONFIG: rule "typecheck" cannot be disabled\n$`},
		{name: "mode flag", args: []string{"-mode", "bogus"}, stderr: `^gglint: unknown mode "bogus" \(want basic, standard, or strict\)\n$`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := workspace(t, tc.toml)
			got := gglint(t, dir, append(append([]string{"run"}, tc.args...), "./a")...)
			configPath := regexp.QuoteMeta(filepath.Join(dir, config.FileName))
			pattern := regexp.MustCompile(strings.ReplaceAll(tc.stderr, "CONFIG", configPath))
			if got.code != 2 || got.stdout != "" || !pattern.MatchString(got.stderr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 2, empty stdout, stderr matching %s",
					got.code, got.stdout, got.stderr, pattern)
			}
		})
	}
}

type jsonFinding struct {
	Rule    string `json:"rule"`
	Check   string `json:"check"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

func (f jsonFinding) token() string {
	if f.Check == "" {
		return f.Rule
	}
	return f.Rule + ":" + f.Check
}

type jsonReport struct {
	Mode     string        `json:"mode"`
	Findings []jsonFinding `json:"findings"`
}

func lintJSON(t *testing.T, dir string, args ...string) (doc jsonReport, stdout string) {
	t.Helper()
	got := gglint(t, dir, append([]string{"run", "-format", "json"}, args...)...)
	if got.code != 1 || got.stderr != "" {
		t.Fatalf("exit %d, stderr %q; want exit 1, empty stderr", got.code, got.stderr)
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("decode %q: %v", got.stdout, err)
	}
	return doc, got.stdout
}

func decodeObject(t *testing.T, data string) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(data), &object); err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	return object
}

func TestRunJSON(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	doc, stdout := lintJSON(t, dir, "./a")
	line, column := position(t, dir, "a/a.go", printfVerb("x"))
	want := jsonFinding{Rule: "printf", Path: "a/a.go", Line: line, Column: column, Message: printfMessage("a", "x")}
	if doc.Mode != "standard" || len(doc.Findings) != 1 || doc.Findings[0] != want {
		t.Fatalf("report %+v; want mode standard, findings [%+v]", doc, want)
	}
	object := decodeObject(t, stdout)
	findings, ok := object["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("findings is not a non-empty array: %s", stdout)
	}
	first, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("finding is not an object: %s", stdout)
	}
	_, hasConfig := object["config"]
	_, hasCheck := first["check"]
	if hasConfig || hasCheck {
		t.Errorf("empty config and check must be omitted: %s", stdout)
	}
}

func TestRunJSONClean(t *testing.T) {
	t.Parallel()
	got := gglint(t, workspace(t, ""), "run", "-format", "json", "./b")
	if got.code != 0 || got.stderr != "" {
		t.Fatalf("exit %d, stderr %q; want exit 0, empty stderr", got.code, got.stderr)
	}
	object := decodeObject(t, got.stdout)
	findings, isArray := object["findings"].([]any)
	if len(object) != 2 || object["mode"] != "standard" || !isArray || len(findings) != 0 {
		t.Errorf("clean report %s; want only mode standard and findings []", got.stdout)
	}
}

var modes = []config.Mode{config.Basic, config.Standard, config.Strict}

func findAt(doc jsonReport, rule, path string, line int) (jsonFinding, bool) {
	for _, f := range doc.Findings {
		if f.Rule == rule && f.Path == path && f.Line == line {
			return f, true
		}
	}
	return jsonFinding{}, false
}

func tokens(doc jsonReport) map[string]bool {
	out := make(map[string]bool)
	for _, f := range doc.Findings {
		out[f.token()] = true
	}
	return out
}

func TestModes(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	reports := make(map[config.Mode]jsonReport, len(modes))
	for _, m := range modes {
		reports[m], _ = lintJSON(t, dir, "-mode", m.String(), "./...")
		if reports[m].Mode != m.String() {
			t.Errorf("mode %s report names mode %q", m, reports[m].Mode)
		}
	}
	for i := 1; i < len(modes); i++ {
		lower, upper := tokens(reports[modes[i-1]]), tokens(reports[modes[i]])
		for token := range lower {
			if !upper[token] {
				t.Errorf("token %s reported in %s but not in %s", token, modes[i-1], modes[i])
			}
		}
	}
	assertConcerns(t, dir, reports)
}

func assertConcerns(t *testing.T, dir string, reports map[config.Mode]jsonReport) {
	t.Helper()
	concerns := []struct {
		rule    string
		mode    config.Mode
		file    string
		needle  string
		message string
	}{
		{"goroutine", config.Standard, "d/d.go", "go work()", "goroutine is not joined: use errgroup.Group.Go or sync.WaitGroup.Go"},
		{"capturerace", config.Standard, "e/e.go", "go func()", "s is written after a goroutine that captures it starts: pass it as an argument or synchronize first"},
		{"complexity", config.Standard, "f/f.go", "func Branchy", "function Branchy is too complex: cyclomatic complexity 17 exceeds 15"},
		{"chanblock", config.Strict, "g/g.go", "ch <- 1", "channel send can block forever: select on ctx.Done() or use a buffered channel"},
		{"placeholder", config.Standard, "h/h.go", "panic(", "placeholder panic: implement this path"},
	}
	for _, c := range concerns {
		line, _ := position(t, dir, c.file, c.needle)
		for _, m := range modes {
			f, found := findAt(reports[m], c.rule, c.file, line)
			if found != (m >= c.mode) {
				t.Errorf("mode %s: %s finding at %s:%d present=%t, want %t", m, c.rule, c.file, line, found, m >= c.mode)
			}
			if m == c.mode && found && f.Message != c.message {
				t.Errorf("mode %s: %s message %q, want %q", m, c.rule, f.Message, c.message)
			}
		}
	}
}

func TestRulesSelected(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	for _, m := range modes {
		t.Run(m.String(), func(t *testing.T) {
			t.Parallel()
			got := gglint(t, dir, "rules", "-selected", "-mode", m.String())
			if got.code != 0 || got.stderr != "" {
				t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
			}
			var names []string
			for line := range strings.Lines(got.stdout) {
				name, _, _ := strings.Cut(line, "\t")
				names = append(names, name)
			}
			want := selectedNames(t, dir, m)
			if strings.Join(names, " ") != strings.Join(want, " ") {
				t.Errorf("rules -selected -mode %s:\n got %v\nwant %v", m, names, want)
			}
		})
	}
}

func selectedNames(t *testing.T, dir string, m config.Mode) []string {
	t.Helper()
	path, err := config.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, dir, m.String())
	if err != nil {
		t.Fatal(err)
	}
	selected, err := rules.Select(cfg)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(selected))
	for _, r := range selected {
		names = append(names, r.Name)
	}
	return names
}

func TestFix(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	unformatted := "i/i.go:1:1: file is not gofumpt-formatted [gofumpt]\n\tfix: format with gofumpt\n"
	assertResult(t, gglint(t, dir, "run", "./i"), result{code: 1, stdout: unformatted, stderr: summary("", 1)})
	assertResult(t, gglint(t, dir, "run", "-fix", "./i"), result{})
	assertResult(t, gglint(t, dir, "run", "./i"), result{})
}

func TestNewFromRev(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := workspace(t, "")
	if err := os.Remove(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	path := filepath.Join(dir, "a", "a.go")
	writeFile(t, path, readFile(t, path)+"\nfunc Worse() {\n\tlogf(\"%d\\n\", \"z\")\n}\n")
	added := expect{file: "a/a.go", needle: printfVerb("z"), message: printfMessage("a", "z"), token: "printf"}
	got := gglint(t, dir, "run", "-new-from-rev", "HEAD", "./a")
	assertResult(t, got, result{code: 1, stdout: added.text(t, dir), stderr: summary("", 1)})
}
