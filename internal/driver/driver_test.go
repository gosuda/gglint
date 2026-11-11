package driver_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
	"github.com/gosuda/gglint/internal/finding"
)

const fakeGo = `#!/bin/sh
d=$(dirname "$0")
printf '%s\n' "$@" >"$d/args"
pwd -P >"$d/pwd"
cat "$d/stdout"
cat "$d/stderr" >&2
exit "$(cat "$d/code")"
`

type goRun struct {
	stdout, stderr string
	code           int
}

func installGo(t *testing.T, run goRun) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake go command needs a POSIX shell")
	}
	dir := t.TempDir()
	files := map[string]string{"go": fakeGo, "stdout": run.stdout, "stderr": run.stderr, "code": strconv.Itoa(run.code)}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func invocation(work string) driver.Invocation {
	return driver.Invocation{Exe: "/opt/gglint", ConfigFile: "/cache/config.json", WorkDir: work, Patterns: []string{"./..."}}
}

func TestVetMapsEachDiagnosticFieldOntoTheFinding(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	cases := []struct {
		name, diagnostic string
		want             finding.Finding
	}{
		{
			name:       "position and distinct end",
			diagnostic: `{"posn":"/w/a.go:5:2","end":"/w/a.go:5:20","message":"bad verb"}`,
			want:       finding.Finding{Message: "bad verb", Pos: pos("/w/a.go", 5, 2), End: pos("/w/a.go", 5, 20)},
		},
		{
			name:       "position without column keeps path with spaces and line",
			diagnostic: `{"posn":"/w/a b.go:12","message":"m"}`,
			want:       finding.Finding{Message: "m", Pos: pos("/w/a b.go", 12, 0)},
		},
		{
			name:       "fixable when any fix has edits, messages kept in order",
			diagnostic: `{"posn":"/w/a.go:1:1","message":"m","suggested_fixes":[{"message":"hint","edits":[]},{"message":"fix it","edits":[{"filename":"/w/a.go","start":1,"end":2,"new":"x"}]}]}`,
			want:       finding.Finding{Message: "m", Pos: pos("/w/a.go", 1, 1), Fixes: []string{"hint", "fix it"}, Fixable: true},
		},
		{
			name:       "fixes without edits are not fixable",
			diagnostic: `{"posn":"/w/a.go:1:1","message":"m","suggested_fixes":[{"message":"hint","edits":[]}]}`,
			want:       finding.Finding{Message: "m", Pos: pos("/w/a.go", 1, 1), Fixes: []string{"hint"}},
		},
		{
			name:       "fix with edits and no message stays fixable",
			diagnostic: `{"posn":"/w/a.go:1:1","message":"m","suggested_fixes":[{"message":"","edits":[{"filename":"/w/a.go","start":0,"end":1,"new":""}]}]}`,
			want:       finding.Finding{Message: "m", Pos: pos("/w/a.go", 1, 1), Fixes: []string{""}, Fixable: true},
		},
		{
			name:       "related positions in order",
			diagnostic: `{"posn":"/w/a.go:9:3","message":"m","related":[{"posn":"/w/b.go:1:4","message":"first"},{"posn":"/w/a.go:2","message":"second"}]}`,
			want: finding.Finding{Message: "m", Pos: pos("/w/a.go", 9, 3), Related: []finding.Related{
				{Position: pos("/w/b.go", 1, 4), Message: "first"},
				{Position: pos("/w/a.go", 2, 0), Message: "second"},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PATH", installGo(t, goRun{stdout: `{"m/a":{"modernize_forvar":[` + c.diagnostic + `]}}` + "\n"})+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := driver.Vet(t.Context(), invocation(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			c.want.Rule = "modernize_forvar"
			if want := []finding.Finding{c.want}; !reflect.DeepEqual(got, want) {
				t.Fatalf("Vet = %+v\nwant %+v", got, want)
			}
		})
	}
}

func pos(path string, line, column int) finding.Position {
	return finding.Position{Path: path, Line: line, Column: column}
}

func TestVetCollectsEveryPackageAndAnalyzerInTheStream(t *testing.T) {
	stream := `{"m/a":{"printf":[{"posn":"/w/a.go:1:1","message":"a1"}],"nilness":[{"posn":"/w/a.go:2:1","message":"a2"}]}}` + "\n" +
		`{"m/b":{"printf":[{"posn":"/w/b.go:1:1","message":"b1"},{"posn":"/w/b.go:2:1","message":"b2"}]}}` + "\n"
	t.Setenv("PATH", installGo(t, goRun{stdout: stream})+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := driver.Vet(t.Context(), invocation(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, f := range got {
		seen = append(seen, f.Rule+"|"+f.Message)
	}
	slices.Sort(seen)
	if want := []string{"nilness|a2", "printf|a1", "printf|b1", "printf|b2"}; !slices.Equal(seen, want) {
		t.Fatalf("findings = %v, want %v", seen, want)
	}
}

func TestVetCleanOutputHasNoFindingsAndNoError(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	for _, stdout := range []string{"", "{}\n", `{"m/a":{}}`, `{"m/a":{"printf":[]}}` + "\n" + `{"m/b":{"nilness":[]}}`} {
		t.Run(strconv.Quote(stdout), func(t *testing.T) {
			t.Setenv("PATH", installGo(t, goRun{stdout: stdout, stderr: "go: downloading example.com/x v1.0.0\n"})+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := driver.Vet(t.Context(), invocation(t.TempDir()))
			if err != nil || len(got) != 0 {
				t.Fatalf("Vet = %+v, %v; want no findings and no error", got, err)
			}
		})
	}
}

func TestVetAnalyzerErrorObjectIsInternalError(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	const failure = `"nilness":{"error":"boom"}`
	cases := map[string]string{
		"alone":                          `{"m/z":{` + failure + `}}`,
		"after another package's output": `{"m/a":{"printf":[{"posn":"/w/a.go:1:1","message":"m"}]}}` + "\n" + `{"m/z":{` + failure + `}}`,
		"beside a diagnostic analyzer":   `{"m/z":{"printf":[{"posn":"/w/a.go:1:1","message":"m"}],` + failure + `}}`,
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PATH", installGo(t, goRun{stdout: stdout})+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := driver.Vet(t.Context(), invocation(t.TempDir()))
			if want := "internal: m/z: nilness: boom"; err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
			if got != nil {
				t.Fatalf("findings returned beside an error: %+v", got)
			}
		})
	}
}

func TestVetMalformedStdoutIsLoadError(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	for _, stdout := range []string{
		`{"m/a":`,
		`[1]`,
		`{} trailing`,
		`{"m/a":{"printf":"text"}}`,
		`{"m/a":{"printf":[{"posn":5}]}}`,
		`{"m/a":{"nilness":{"error":5}}}`,
	} {
		t.Run(stdout, func(t *testing.T) {
			t.Setenv("PATH", installGo(t, goRun{stdout: stdout})+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := driver.Vet(t.Context(), invocation(t.TempDir()))
			if err == nil || !strings.HasPrefix(err.Error(), "load: ") {
				t.Fatalf("error = %v, want a load: error", err)
			}
			if got != nil {
				t.Fatalf("findings returned beside an error: %+v", got)
			}
		})
	}
}

func TestVetTypecheckLineGrammar(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	work := t.TempDir()
	typecheck := func(path string, line, column int, message string) *finding.Finding {
		return &finding.Finding{Rule: finding.TypecheckRule, Message: message, Pos: pos(path, line, column)}
	}
	cases := []struct {
		line string
		want *finding.Finding
	}{
		{"./a.go:4:2: undefined: x", typecheck(filepath.Join(work, "a.go"), 4, 2, "undefined: x")},
		{"b.go:9: declared and not used: y", typecheck(filepath.Join(work, "b.go"), 9, 0, "declared and not used: y")},
		{"vet: c.go:3:1: m", typecheck(filepath.Join(work, "c.go"), 3, 1, "m")},
		{"sub/d.go:5:6: m: with: colons", typecheck(filepath.Join(work, "sub", "d.go"), 5, 6, "m: with: colons")},
		{"../e.go:1:1: m", typecheck(filepath.Join(filepath.Dir(work), "e.go"), 1, 1, "m")},
		{"/abs/f.go:7:8: m", typecheck("/abs/f.go", 7, 8, "m")},
		{"a b.go:2:3: m", typecheck(filepath.Join(work, "a b.go"), 2, 3, "m")},
		{"a.txt:1:2: m", nil},
		{"a.gox:1:2: m", nil},
		{"a.go:x:2: m", nil},
		{"a.go:4:2:m", nil},
		{"a.go: m", nil},
		{"a.go:1:2:", nil},
		{"go: no Go files in /w", nil},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			stderr := "# m/a\n" + c.line + "\n"
			t.Setenv("PATH", installGo(t, goRun{stderr: stderr, code: 1})+string(os.PathListSeparator)+os.Getenv("PATH"))
			got, err := driver.Vet(t.Context(), invocation(work))
			if c.want == nil {
				if want := "load: " + strings.TrimSpace(stderr); err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := []finding.Finding{*c.want}; !reflect.DeepEqual(got, want) {
				t.Fatalf("Vet = %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestVetFailureKeepsAnalyzerFindingsBesideTypecheck(t *testing.T) {
	t.Setenv("PATH", installGo(t, goRun{
		stdout: `{"m/b":{"printf":[{"posn":"/w/b.go:1:1","message":"bad verb"}]}}`,
		stderr: "# m/a\n/w/a.go:4:2: undefined: x\n",
		code:   1,
	})+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := driver.Vet(t.Context(), invocation(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, f := range got {
		seen = append(seen, f.Rule+"|"+f.Message)
	}
	slices.Sort(seen)
	if want := []string{"printf|bad verb", "typecheck|undefined: x"}; !slices.Equal(seen, want) {
		t.Fatalf("findings = %v, want %v", seen, want)
	}
}

func TestGoCommandLine(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	cases := []struct {
		name      string
		fix       bool
		tags      []string
		wantFlags []string
	}{
		{"vet", false, nil, []string{"-config=/cache/config.json", "-json", "-vettool=/opt/gglint"}},
		{"vet with tags", false, []string{"a", "b"}, []string{"-config=/cache/config.json", "-json", "-tags=a,b", "-vettool=/opt/gglint"}},
		{"fix", true, nil, []string{"-config=/cache/config.json", "-fix", "-vettool=/opt/gglint"}},
		{"fix with tags", true, []string{"integration"}, []string{"-config=/cache/config.json", "-fix", "-tags=integration", "-vettool=/opt/gglint"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := installGo(t, goRun{})
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			inv := invocation(t.TempDir())
			inv.BuildTags = c.tags
			inv.Patterns = []string{"./a", "./b/..."}
			var err error
			if c.fix {
				err = driver.Fix(t.Context(), inv)
			} else {
				_, err = driver.Vet(t.Context(), inv)
			}
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(readFile(t, filepath.Join(dir, "args")), "\n"), "\n")
			if args[0] != "vet" || !slices.Equal(args[len(args)-2:], inv.Patterns) {
				t.Fatalf("args = %q, want vet <flags> ./a ./b/...", args)
			}
			flags := slices.Sorted(slices.Values(args[1 : len(args)-2]))
			if !slices.Equal(flags, c.wantFlags) {
				t.Fatalf("flags = %q, want %q", flags, c.wantFlags)
			}
			wd, err := filepath.EvalSymlinks(inv.WorkDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(readFile(t, filepath.Join(dir, "pwd"))); got != wd {
				t.Fatalf("go ran in %q, want %q", got, wd)
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFixErrorClassification(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH"))
	cases := []struct {
		name    string
		run     goRun
		wantErr string
	}{
		{"success", goRun{}, "<nil>"},
		{"failure with a source position", goRun{stderr: "# m/a\n./a.go:1:2: undefined: x\n", code: 1}, "<nil>"},
		{"failure without a source position", goRun{stderr: "\ngo: go.mod file not found\n\n", code: 1}, "load: go: go.mod file not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PATH", installGo(t, c.run)+string(os.PathListSeparator)+os.Getenv("PATH"))
			err := driver.Fix(t.Context(), invocation(t.TempDir()))
			if got := fmt.Sprint(err); got != c.wantErr {
				t.Fatalf("Fix error = %q, want %q", got, c.wantErr)
			}
		})
	}
}

func TestMissingGoCommandIsLoadError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	inv := invocation(t.TempDir())
	if _, err := driver.Vet(t.Context(), inv); err == nil || !strings.HasPrefix(err.Error(), "load: ") {
		t.Fatalf("Vet error = %v, want a load: error", err)
	}
	if err := driver.Fix(t.Context(), inv); err == nil || !strings.HasPrefix(err.Error(), "load: ") {
		t.Fatalf("Fix error = %v, want a load: error", err)
	}
}

func TestCanceledContextIsReportedAsCancellation(t *testing.T) {
	t.Setenv("PATH", installGo(t, goRun{})+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	inv := invocation(t.TempDir())
	got, err := driver.Vet(ctx, inv)
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("Vet = %+v, %v; want no findings and context.Canceled", got, err)
	}
	if err := driver.Fix(ctx, inv); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fix error = %v, want context.Canceled", err)
	}
}
