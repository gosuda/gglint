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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/cli"
	"github.com/gosuda/gglint/internal/config"
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
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	if err := setup(context.Background(), dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

var hermeticGo = []string{
	"GO111MODULE=on",
	"GOTOOLCHAIN=local",
	"GOPROXY=off",
	"GOSUMDB=off",
	"CGO_ENABLED=0",
}

func setup(ctx context.Context, dir string) error {
	binary = filepath.Join(dir, "gglint")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "github.com/gosuda/gglint/cmd/gglint")
	build.Env = append(os.Environ(), hermeticGo...)
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
	env := append(os.Environ(), hermeticGo...)
	return append(env,
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
		b.WriteString("gglint: " + strconv.Itoa(findings) + " findings (mode standard)\n")
	}
	return b.String()
}

func TestRunTextReportsExactlyTheFindingsLeftAfterFiltering(t *testing.T) {
	t.Parallel()
	aFinding := expect{file: "a/a.go", needle: printfVerb("x"), message: printfMessage("a", "x"), token: "printf"}
	cFinding := expect{file: "c/c.go", needle: printfVerb("y"), message: printfMessage("c", "y"), token: "printf"}
	tFinding := expect{file: "t/t.go", needle: printfVerb("x"), message: printfMessage("t", "x"), token: "printf"}
	tTestFinding := expect{file: "t/t_test.go", needle: printfVerb("y"), message: printfMessage("t", "y"), token: "printf"}
	cases := []struct {
		name string
		toml string
		args []string
		want []expect
	}{
		{name: "printf finding", args: []string{"run", "./a"}, want: []expect{aFinding}},
		{name: "clean package", args: []string{"run", "./b"}},
		{name: "two packages sorted by path", args: []string{"run", "./c", "./a"}, want: []expect{aFinding, cFinding}},
		{name: "exclude path", toml: "[[exclude]]\npaths = [\"^c/\"]\n", args: []string{"run", "./a", "./c"}, want: []expect{aFinding}},
		{name: "exclude text", toml: "[[exclude]]\ntext = 'arg \"y\"'\n", args: []string{"run", "./a", "./c"}, want: []expect{aFinding}},
		{name: "exclude fields combine with AND", toml: "[[exclude]]\npaths = [\"^a/\"]\nrules = [\"nolint\"]\n", args: []string{"run", "./a", "./c"}, want: []expect{aFinding, cFinding}},
		{name: "test file finding", args: []string{"run", "./t"}, want: []expect{tFinding, tTestFinding}},
		{name: "run tests off drops test files", toml: "[run]\ntests = false\n", args: []string{"run", "./t"}, want: []expect{tFinding}},
		{name: "exclude tests", toml: "[[exclude]]\ntests = true\nrules = [\"printf\"]\n", args: []string{"run", "./t"}, want: []expect{tFinding}},
		{name: "exclude non-tests", toml: "[[exclude]]\ntests = false\nrules = [\"printf\"]\n", args: []string{"run", "./t"}, want: []expect{tTestFinding}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := workspace(t, tc.toml)
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

func TestRunFromSubdirectoryPrintsCwdRelativePathsAndExcludesConfigRelative(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "[[exclude]]\npaths = [\"^a/\"]\n")
	sub := filepath.Join(dir, "a")
	c := expect{file: "../c/c.go", needle: printfVerb("y"), message: printfMessage("c", "y"), token: "printf"}
	want := result{code: 1, stdout: c.text(t, sub), stderr: "gglint: config ../gglint.toml\ngglint: 1 findings (mode standard)\n"}
	assertResult(t, gglint(t, sub, "run", ".", "../c"), want)
}

func TestConfigErrorExitsTwoWithEmptyStdoutDespiteFindings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		toml   string
		args   []string
		stderr string
	}{
		{name: "unknown key", toml: "bogus = 1\n", stderr: `^gglint: CONFIG: 1:1: unknown key bogus\n$`},
		{name: "unknown mode", toml: "mode = \"loose\"\n", stderr: `^gglint: CONFIG: unknown mode "loose" \(want basic, standard, or strict\)\n$`},
		{name: "unknown enable", toml: "enable = [\"nosuch\"]\n", stderr: `^gglint: CONFIG: enable: unknown rule "nosuch"\n$`},
		{name: "unknown disable", toml: "disable = [\"nosuch\"]\n", stderr: `^gglint: CONFIG: disable: unknown rule "nosuch"\n$`},
		{name: "enable and disable", toml: "enable = [\"chanblock\"]\ndisable = [\"chanblock\"]\n", stderr: `^gglint: CONFIG: rule "chanblock" is in both enable and disable\n$`},
		{name: "disable typecheck", toml: "disable = [\"typecheck\"]\n", stderr: `^gglint: CONFIG: rule "typecheck" cannot be disabled\n$`},
		{name: "mode flag", args: []string{"-mode", "bogus"}, stderr: `^gglint: unknown mode "bogus" \(want basic, standard, or strict\)\n$`},
		{name: "empty exclude", toml: "[[exclude]]\n", stderr: `^gglint: CONFIG: exclude\[0\]: set at least one of paths, rules, tests, text\n$`},
		{name: "bad exclude path", toml: "[[exclude]]\npaths = [\"^a/\", \"(\"]\n", stderr: `^gglint: CONFIG: exclude\[0\]\.paths\[1\]: error parsing regexp: [^\n]+\n$`},
		{name: "bad exclude text", toml: "[[exclude]]\ntext = \"[\"\n", stderr: `^gglint: CONFIG: exclude\[0\]\.text: error parsing regexp: [^\n]+\n$`},
		{name: "unknown exclude rule", toml: "[[exclude]]\npaths = [\"^a/\"]\n\n[[exclude]]\nrules = [\"nosuch\"]\n", stderr: `^gglint: CONFIG: exclude\[1\]\.rules: unknown rule "nosuch"\n$`},
		{name: "exclude typecheck rule", toml: "[[exclude]]\nrules = [\"printf\", \"typecheck\"]\n", stderr: `^gglint: CONFIG: exclude\[0\]\.rules: typecheck findings cannot be excluded\n$`},
		{name: "exclude typecheck check", toml: "[[exclude]]\npaths = [\"^a/\"]\n\n[[exclude]]\nrules = [\"typecheck:x\"]\n", stderr: `^gglint: CONFIG: exclude\[1\]\.rules: typecheck findings cannot be excluded\n$`},
		{name: "negative complexity limit", toml: "[rules.complexity]\nmax_lines = -1\n", stderr: `^gglint: CONFIG: rules\.complexity\.max_lines: must be >= 0\n$`},
		{name: "empty deny path", toml: "[rules.checkimports]\ndeny = [{ path = \"\" }]\n", stderr: `^gglint: CONFIG: rules\.checkimports\.deny\[0\]: path is empty\n$`},
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

func TestRunJSONReportsFindingFieldsAndOmitsEmptyConfigAndCheck(t *testing.T) {
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

func TestRunJSONCleanRunHasOnlyModeAndEmptyFindings(t *testing.T) {
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

func TestModesNestTokensGateConcernsAndScopeNolintDirectives(t *testing.T) {
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
	assertNolint(t, dir, reports)
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

var (
	modeNames   = []string{"basic", "standard", "strict"}
	catalogSize = [...]int{54, 101, 122}
	origins     = []string{"go/analysis", "staticcheck", "third-party", "gvisor", "logos", "gglint"}
)

type ruleLine struct {
	name string
	mode int
}

func (r ruleLine) before(o ruleLine) bool {
	return r.mode < o.mode || r.mode == o.mode && r.name < o.name
}

func listRules(t *testing.T, dir string, args ...string) []ruleLine {
	t.Helper()
	got := gglint(t, dir, append([]string{"rules"}, args...)...)
	if got.code != 0 || !onlyGglintLines(got.stderr) {
		t.Fatalf("rules %v: exit %d, stderr %q; want exit 0 and only gglint: lines", args, got.code, got.stderr)
	}
	var out []ruleLine
	for line := range strings.Lines(got.stdout) {
		fields := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(fields) != 4 || fields[0] == "" || fields[3] == "" || !slices.Contains(origins, fields[2]) || !slices.Contains(modeNames, fields[1]) {
			t.Fatalf("rules %v: line %q is not name<TAB>mode<TAB>origin<TAB>description", args, line)
		}
		r := ruleLine{name: fields[0], mode: slices.Index(modeNames, fields[1])}
		if n := len(out); n > 0 && !out[n-1].before(r) {
			t.Fatalf("rules %v: %v listed after %v; want strictly sorted by mode, then name", args, r, out[n-1])
		}
		out = append(out, r)
	}
	return out
}

func onlyGglintLines(stderr string) bool {
	for line := range strings.Lines(stderr) {
		if !strings.HasPrefix(line, "gglint: ") {
			return false
		}
	}
	return true
}

func where(all []ruleLine, keep func(ruleLine) bool) []ruleLine {
	return slices.DeleteFunc(slices.Clone(all), func(r ruleLine) bool { return !keep(r) })
}

func assertRules(t *testing.T, got, want []ruleLine) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("rules:\n got %v\nwant %v", got, want)
	}
}

func TestRulesCatalogIsNestedByModeAndIgnoresConfig(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "mode = \"loose\"\n")
	all := listRules(t, dir)
	if len(all) != catalogSize[config.Strict] {
		t.Fatalf("catalog has %d rules, want %d", len(all), catalogSize[config.Strict])
	}
	for _, m := range modes {
		want := where(all, func(r ruleLine) bool { return r.mode <= int(m) })
		if len(want) != catalogSize[m] {
			t.Errorf("mode %s: %d rules at or below it, want %d", m, len(want), catalogSize[m])
		}
		assertRules(t, listRules(t, dir, "-mode", m.String()), want)
	}
	got := gglint(t, dir, "rules", "-selected")
	wantErr := "gglint: " + filepath.Join(dir, config.FileName) + ": unknown mode \"loose\" (want basic, standard, or strict)\n"
	assertResult(t, got, result{code: 2, stderr: wantErr})
}

func TestRulesSelectedAppliesModeThenEnableThenDisable(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	all := listRules(t, dir)
	for _, m := range modes {
		assertRules(t, listRules(t, dir, "-selected", "-mode", m.String()), where(all, func(r ruleLine) bool { return r.mode <= int(m) }))
	}
	assertRules(t, listRules(t, dir, "-selected"), where(all, func(r ruleLine) bool { return r.mode <= int(config.Standard) }))

	writeFile(t, filepath.Join(dir, config.FileName), "mode = \"basic\"\nenable = [\"chanblock\"]\ndisable = [\"printf\"]\n")
	adjusted := func(m int) []ruleLine {
		return where(all, func(r ruleLine) bool { return (r.mode <= m || r.name == "chanblock") && r.name != "printf" })
	}
	assertRules(t, listRules(t, dir, "-selected"), adjusted(int(config.Basic)))
	for _, m := range modes {
		assertRules(t, listRules(t, dir, "-selected", "-mode", m.String()), adjusted(int(m)))
	}

	writeFile(t, filepath.Join(dir, "a", "other.toml"), "mode = \"strict\"\ndisable = [\"chanblock\"]\n")
	assertRules(t, listRules(t, dir, "-selected", "-config", "a/other.toml"), where(all, func(r ruleLine) bool { return r.name != "chanblock" }))
}

func located(doc jsonReport, keep func(jsonFinding) bool) []string {
	var out []string
	for _, f := range doc.Findings {
		if keep(f) {
			out = append(out, f.token()+" "+f.Path+":"+strconv.Itoa(f.Line))
		}
	}
	slices.Sort(out)
	return out
}

func at(t *testing.T, dir, token, file, needle string) string {
	t.Helper()
	line, _ := position(t, dir, file, needle)
	return token + " " + file + ":" + strconv.Itoa(line)
}

func messagesAt(doc jsonReport, token, path string, line int) []string {
	var out []string
	for _, f := range doc.Findings {
		if f.token() == token && f.Path == path && f.Line == line {
			out = append(out, f.Message)
		}
	}
	slices.Sort(out)
	return out
}

const (
	needsReason = "nolint directive needs a reason after //"
	mustName    = "nolint directive must name rules"
)

var (
	always = [3]bool{true, true, true}
	never  = [3]bool{}
)

func inAll(messages ...string) [3][]string {
	return [3][]string{messages, messages, messages}
}

type directiveCase struct {
	name   string
	needle string
	nolint [3][]string
	kept   map[string][3]bool
}

var directiveCases = []directiveCase{
	{"named directive without reason is reported only in strict", `"noreason")`, [3][]string{nil, nil, {needsReason}}, map[string][3]bool{"printf": never}},
	{"bare directive must name rules from standard and needs a reason in strict", `"bare")`, [3][]string{nil, {mustName}, {mustName, needsReason}}, nil},
	{"spaced directive is malformed in every mode and suppresses nothing", `"spaced")`, inAll("malformed nolint directive"), map[string][3]bool{"printf": always}},
	{"unknown rule is reported in every mode and suppresses nothing", `"unknown")`, inAll(`unknown nolint rule "nope"`), map[string][3]bool{"printf": always}},
	{"directive matching nothing is unused in every mode", `"unused")`, inAll("unused nolint directive for printf"), nil},
	{"directive naming an unselected rule is unused only where the rule is selected", `"unselected")`, [3][]string{nil, nil, {"unused nolint directive for chanblock"}}, nil},
	{"typecheck token is rejected in every mode", `"typecheck")`, inAll("typecheck findings cannot be suppressed"), nil},
	{"rule:check token suppresses its own check", "staticcheck:SA4000 //", [3][]string{}, map[string][3]bool{"staticcheck:SA4000": never}},
	{"rule:check token leaves other checks of the rule reported", "staticcheck:SA4001 //", inAll("unused nolint directive for staticcheck:SA4001"), map[string][3]bool{"staticcheck:SA4000": always}},
	{"rule token suppresses every check of the rule", "//nolint:staticcheck //", [3][]string{}, map[string][3]bool{"staticcheck:SA4000": never}},
	{"directive naming an unmatched selected rule reports it unused and still suppresses the matched rule", `"errcheckpair")`, inAll("unused nolint directive for errcheck"), map[string][3]bool{"printf": never}},
	{"trailing comma is malformed in every mode and suppresses nothing", `"trailingcomma")`, inAll("malformed nolint directive"), map[string][3]bool{"printf": always}},
	{"reason without slashes is malformed in every mode and suppresses nothing", `"noslash")`, inAll("malformed nolint directive"), map[string][3]bool{"printf": always}},
	{"typecheck token beside a selected rule is rejected and the rule is unused", `"typecheckpair")`, inAll("typecheck findings cannot be suppressed", "unused nolint directive for printf"), nil},
}

func assertNolint(t *testing.T, dir string, reports map[config.Mode]jsonReport) {
	t.Helper()
	t.Run("directive covers its line, next statement, or file and nothing else", func(t *testing.T) {
		t.Parallel()
		want := []string{at(t, dir, "printf", "n/n.go", `"below trailing"`), at(t, dir, "printf", "n/n.go", `"below statement"`)}
		slices.Sort(want)
		for _, m := range modes {
			got := located(reports[m], func(f jsonFinding) bool {
				return strings.HasPrefix(f.Path, "n/") && (f.Rule == "printf" || f.Rule == "nolint")
			})
			if !slices.Equal(got, want) {
				t.Errorf("mode %s: package n findings %v, want %v", m, got, want)
			}
		}
	})
	for _, c := range directiveCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			line, _ := position(t, dir, "p/p.go", c.needle)
			for _, m := range modes {
				want := slices.Sorted(slices.Values(c.nolint[m]))
				if got := messagesAt(reports[m], "nolint", "p/p.go", line); !slices.Equal(got, want) {
					t.Errorf("mode %s: nolint messages at p/p.go:%d = %q, want %q", m, line, got, want)
				}
				for token, kept := range c.kept {
					if got := len(messagesAt(reports[m], token, "p/p.go", line)) > 0; got != kept[m] {
						t.Errorf("mode %s: %s finding at p/p.go:%d reported = %t, want %t", m, token, line, got, kept[m])
					}
				}
			}
		})
	}
}

const brokenSource = "package x\n\nfunc Broken() int {\n\treturn \"s\" //nolint:typecheck // reason\n}\n"

func TestTypecheckFindingIsUnsuppressibleAndExitsOneAlongsideAnalyzerFindings(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	if err := os.Mkdir(filepath.Join(dir, "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "x", "x.go"), brokenSource)
	doc, _ := lintJSON(t, dir, "./a", "./x")
	want := []string{at(t, dir, "printf", "a/a.go", printfVerb("x")), at(t, dir, "typecheck", "x/x.go", "return")}
	if got := located(doc, func(jsonFinding) bool { return true }); !slices.Equal(got, want) {
		t.Errorf("findings %v, want %v", got, want)
	}
}

func TestNonReportExitsLeaveStdoutEmpty(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	oneGglintLine := `^gglint: [^\n]+\n$`
	cases := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{name: "no command", code: 2, stderr: oneGglintLine},
		{name: "unknown command", args: []string{"lint"}, code: 2, stderr: oneGglintLine},
		{name: "undefined run flag", args: []string{"run", "-bogus", "./a"}, code: 2, stderr: `-bogus`},
		{name: "run help lists its flags", args: []string{"run", "-h"}, code: 2, stderr: `(?s)-config.*-fix.*-format.*-mode.*-new-from-rev`},
		{name: "rules help lists its flags", args: []string{"rules", "-h"}, code: 2, stderr: `(?s)-config.*-mode.*-selected`},
		{name: "unknown format", args: []string{"run", "-format", "xml", "./a"}, code: 2, stderr: oneGglintLine},
		{name: "rules config without selected", args: []string{"rules", "-config", config.FileName}, code: 2, stderr: oneGglintLine},
		{name: "rules unknown mode", args: []string{"rules", "-mode", "loose"}, code: 2, stderr: `^gglint: unknown mode "loose" \(want basic, standard, or strict\)\n$`},
		{name: "missing package directory", args: []string{"run", "./nosuchdir"}, code: 3, stderr: `^gglint: load: `},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := gglint(t, dir, tc.args...)
			if got.code != tc.code || got.stdout != "" || !regexp.MustCompile(tc.stderr).MatchString(got.stderr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit %d, empty stdout, stderr matching %s",
					got.code, got.stdout, got.stderr, tc.code, tc.stderr)
			}
		})
	}
}

func TestVersionPrintsModuleVersionAndBuildGoVersion(t *testing.T) {
	t.Parallel()
	got := gglint(t, t.TempDir(), "version")
	fields := strings.Fields(got.stdout)
	if got.code != 0 || got.stderr != "" || len(fields) != 3 || fields[0] != "gglint" || fields[2] != runtime.Version() || got.stdout != strings.Join(fields, " ")+"\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 0 and one line gglint <version> %s", got.code, got.stdout, got.stderr, runtime.Version())
	}
}

func TestFixAppliesSuggestedEditsThenReportsClean(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	unformatted := "i/i.go:1:1: file is not gofumpt-formatted [gofumpt]\n\tfix: format with gofumpt\n"
	assertResult(t, gglint(t, dir, "run", "./i"), result{code: 1, stdout: unformatted, stderr: summary("", 1)})
	assertResult(t, gglint(t, dir, "run", "-fix", "./i"), result{})
	if got, want := readFile(t, filepath.Join(dir, "i", "i.go")), "package i\n\nfunc One() int {\n\treturn 1\n}\n"; got != want {
		t.Errorf("fixed i/i.go = %q, want %q", got, want)
	}
	assertResult(t, gglint(t, dir, "run", "./i"), result{})
}

func gitWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := workspace(t, "")
	if err := os.Remove(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, content)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	return dir
}

func TestNewFromRevKeepsChangedLinesAndEveryTypecheckFinding(t *testing.T) {
	t.Parallel()
	dir := gitWorkspace(t, map[string]string{"x/x.go": brokenSource})
	path := filepath.Join(dir, "a", "a.go")
	writeFile(t, path, readFile(t, path)+"\nfunc Worse() {\n\tlogf(\"%d\\n\", \"z\")\n}\n")
	doc, _ := lintJSON(t, dir, "-new-from-rev", "HEAD", "./a", "./x")
	want := []string{at(t, dir, "printf", "a/a.go", printfVerb("z")), at(t, dir, "typecheck", "x/x.go", "return")}
	if got := located(doc, func(jsonFinding) bool { return true }); !slices.Equal(got, want) {
		t.Errorf("findings %v, want %v", got, want)
	}
	got := gglint(t, dir, "run", "-new-from-rev", "no-such-rev", "./a")
	if got.code != 3 || got.stdout != "" || !strings.HasPrefix(got.stderr, "gglint: diff: ") {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 3, empty stdout, gglint: diff: error", got.code, got.stdout, got.stderr)
	}
}

func TestNewFromRevOptionLikeRevisionExitsThreeWithoutRunningGit(t *testing.T) {
	t.Parallel()
	const kept = "keep me\n"
	dir := gitWorkspace(t, map[string]string{"victim.txt": kept})
	path := filepath.Join(dir, "a", "a.go")
	writeFile(t, path, readFile(t, path)+"\nfunc Worse() {\n\tlogf(\"%d\\n\", \"z\")\n}\n")
	victim := filepath.Join(dir, "victim.txt")
	for _, rev := range []string{"--output=" + victim, "-p"} {
		got := gglint(t, dir, "run", "-new-from-rev="+rev, "./a")
		assertResult(t, got, result{code: 3, stderr: "gglint: diff: revision must not start with '-'\n"})
		if content := readFile(t, victim); content != kept {
			t.Fatalf("-new-from-rev=%s: victim.txt = %q, want %q", rev, content, kept)
		}
	}
}

func TestGeneratedFilesDropAnalyzerFindingsButKeepTypecheck(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	doc, _ := lintJSON(t, dir, "./gen", "./genbroken")
	want := []string{at(t, dir, "printf", "gen/plain.go", printfVerb("plain")), at(t, dir, "typecheck", "genbroken/gen.go", "return")}
	if got := located(doc, func(jsonFinding) bool { return true }); !slices.Equal(got, want) {
		t.Errorf("findings %v, want %v", got, want)
	}
}

func TestFixWithTestsOffLeavesTestFilesByteIdentical(t *testing.T) {
	t.Parallel()
	const testsOff = "[run]\ntests = false\n"
	dir := workspace(t, "")
	unformatted := func(file string) string {
		return file + ":1:1: file is not gofumpt-formatted [gofumpt]\n\tfix: format with gofumpt\n"
	}
	both := unformatted("fixtests/f.go") + unformatted("fixtests/f_test.go")
	assertResult(t, gglint(t, dir, "run", "./fixtests"), result{code: 1, stdout: both, stderr: summary("", 2)})
	testFile := filepath.Join(dir, "fixtests", "f_test.go")
	before := readFile(t, testFile)
	writeFile(t, filepath.Join(dir, config.FileName), testsOff)
	assertResult(t, gglint(t, dir, "run", "-fix", "./fixtests"), result{stderr: summary(testsOff, 0)})
	if got, want := readFile(t, filepath.Join(dir, "fixtests", "f.go")), "package fixtests\n\nfunc Two() int {\n\treturn 2\n}\n"; got != want {
		t.Errorf("fixed fixtests/f.go = %q, want %q", got, want)
	}
	if got := readFile(t, testFile); got != before {
		t.Errorf("fixtests/f_test.go changed with tests = false: %q, want %q", got, before)
	}
}

func TestConfigReadFailureExitsThreeAndMissingExplicitConfigExitsTwo(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "mode = \"basic\"\n")
	missing := filepath.Join(dir, "nosuch.toml")
	notFound := "gglint: " + missing + ": open " + missing + ": no such file or directory\n"
	for _, args := range [][]string{{"run", "-config", "nosuch.toml", "./b"}, {"rules", "-selected", "-config", "nosuch.toml"}} {
		assertResult(t, gglint(t, dir, args...), result{code: 2, stderr: notFound})
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads files regardless of permission bits")
	}
	path := filepath.Join(dir, config.FileName)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	denied := "gglint: internal: " + path + ": open " + path + ": permission denied\n"
	for _, args := range [][]string{{"run", "./b"}, {"rules", "-selected"}} {
		assertResult(t, gglint(t, dir, args...), result{code: 3, stderr: denied})
	}
}

func TestMainWithCanceledContextExitsThreeInterrupted(t *testing.T) {
	dir := workspace(t, "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Chdir(dir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr strings.Builder
	code := cli.Main(ctx, []string{"run", "./b"}, &stdout, &stderr)
	assertResult(t, result{code: code, stdout: stdout.String(), stderr: stderr.String()}, result{code: 3, stderr: "gglint: interrupted\n"})
}
