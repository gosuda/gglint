package finding_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/finding"
)

var work = filepath.FromSlash("/w/repo")

func abs(slashPath string) string {
	return filepath.FromSlash(slashPath)
}

func at(path string, line, column int) finding.Position {
	return finding.Position{Path: path, Line: line, Column: column}
}

func process(t *testing.T, fs []finding.Finding, opts finding.Options) []finding.Finding {
	t.Helper()
	out, err := finding.Process(t.Context(), fs, opts)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func keys(fs []finding.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s:%d:%d %s %s", f.Pos.Path, f.Pos.Line, f.Pos.Column, f.Token(), f.Message))
	}
	return out
}

func TestProcessPrintsEveryPathRelativeToWorkDir(t *testing.T) {
	cases := map[string]string{
		"/w/repo/pkg/x.go": "pkg/x.go",
		"/w/repo/x.go":     "x.go",
		"/w/other/y.go":    "../other/y.go",
		"/z.go":            "../../z.go",
		"/w/repo2/v.go":    "../repo2/v.go",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			f := finding.Finding{
				Rule: "a", Message: "m", Pos: at(abs(in), 3, 2), End: at(abs(in), 3, 9),
				Related: []finding.Related{{Position: at(abs(in), 1, 1), Message: "r"}},
			}
			out := process(t, []finding.Finding{f}, finding.Options{WorkDir: work, Tests: true})
			got := []string{out[0].Pos.Path, out[0].End.Path, out[0].Related[0].Path}
			if !slices.Equal(got, []string{want, want, want}) {
				t.Fatalf("pos, end, related paths = %q, want %q for each", got, want)
			}
		})
	}
}

func TestProcessMissingColumnPrintsOne(t *testing.T) {
	for column, want := range map[int]int{0: 1, 1: 1, 2: 2, 17: 17} {
		t.Run(strconv.Itoa(column), func(t *testing.T) {
			f := finding.Finding{
				Rule: "a", Message: "m", Pos: at(abs("/w/repo/a.go"), 4, column),
				Related: []finding.Related{{Position: at(abs("/w/repo/a.go"), 2, column), Message: "r"}},
			}
			out := process(t, []finding.Finding{f}, finding.Options{WorkDir: work, Tests: true})
			if out[0].Pos.Column != want || out[0].Related[0].Column != want {
				t.Fatalf("column %d printed as pos %d, related %d; want %d", column, out[0].Pos.Column, out[0].Related[0].Column, want)
			}
		})
	}
}

func TestProcessTestsFalseDropsExactlyTestFileFindings(t *testing.T) {
	cases := []struct {
		rule, path, related string
		dropped             bool
	}{
		{"a", "/w/repo/x_test.go", "/w/repo/x_test.go", true},
		{"a", "/w/repo/sub/x_test.go", "/w/repo/x.go", true},
		{"a", "/w/other_test.go", "/w/other_test.go", true},
		{finding.TypecheckRule, "/w/repo/y_test.go", "/w/repo/y_test.go", true},
		{"a", "/w/repo/x.go", "/w/repo/x.go", false},
		{"a", "/w/repo/x_test_helper.go", "/w/repo/x_test_helper.go", false},
		{"a", "/w/repo/xtest.go", "/w/repo/xtest.go", false},
		{"a", "/w/repo/test.go", "/w/repo/test.go", false},
		{"a", "/w/repo/x_test.go.orig", "/w/repo/x_test.go.orig", false},
		{"a", "/w/repo/x.go", "/w/repo/x_test.go", false},
		{finding.TypecheckRule, "/w/repo/y.go", "/w/repo/y.go", false},
	}
	for _, c := range cases {
		t.Run(c.rule+" "+c.path+" "+c.related, func(t *testing.T) {
			fs := []finding.Finding{{
				Rule: c.rule, Message: "m", Pos: at(abs(c.path), 1, 1),
				Related: []finding.Related{{Position: at(abs(c.related), 1, 1), Message: "r"}},
			}}
			if got := len(process(t, fs, finding.Options{WorkDir: work, Tests: true})); got != 1 {
				t.Fatalf("tests=true kept %d findings, want 1", got)
			}
			if kept := len(process(t, fs, finding.Options{WorkDir: work})) == 1; kept == c.dropped {
				t.Fatalf("tests=false kept=%v, want kept=%v", kept, !c.dropped)
			}
		})
	}
}

func TestProcessDedupeKeyIsTokenPathLineColumnMessage(t *testing.T) {
	base := finding.Finding{Rule: "a", Message: "m", Pos: at(abs("/w/repo/a.go"), 3, 1)}
	other := finding.Finding{Rule: "z", Message: "z", Pos: at(abs("/w/repo/z.go"), 1, 1)}
	cases := []struct {
		name   string
		mutate func(*finding.Finding)
		want   int
	}{
		{"identical", func(*finding.Finding) {}, 2},
		{"rule differs", func(f *finding.Finding) { f.Rule = "b" }, 3},
		{"check differs", func(f *finding.Finding) { f.Check = "c" }, 3},
		{"path differs", func(f *finding.Finding) { f.Pos.Path = abs("/w/repo/b.go") }, 3},
		{"line differs", func(f *finding.Finding) { f.Pos.Line = 4 }, 3},
		{"column differs", func(f *finding.Finding) { f.Pos.Column = 3 }, 3},
		{"message differs", func(f *finding.Finding) { f.Message = "n" }, 3},
		{"missing column prints as the same column 1", func(f *finding.Finding) { f.Pos.Column = 0 }, 2},
		{"end differs", func(f *finding.Finding) { f.End = at(abs("/w/repo/a.go"), 3, 9) }, 2},
		{"fixes differ", func(f *finding.Finding) { f.Fixes, f.Fixable = []string{"fix"}, true }, 2},
		{"related differs", func(f *finding.Finding) {
			f.Related = []finding.Related{{Position: at(abs("/w/repo/a.go"), 1, 1), Message: "r"}}
		}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			twin := base
			c.mutate(&twin)
			out := process(t, []finding.Finding{base, other, twin, base}, finding.Options{WorkDir: work, Tests: true})
			if len(out) != c.want {
				t.Fatalf("kept %v, want %d findings", keys(out), c.want)
			}
		})
	}
}

func TestProcessSortIsPathLineColumnTokenMessageForAnyInputOrder(t *testing.T) {
	root := abs("/w/m")
	in := []finding.Finding{
		{Rule: "a", Message: "m", Pos: at(abs("/w/m/a.go"), 10, 10)},
		{Rule: "a", Message: "m", Pos: at(abs("/x.go"), 1, 1)},
		{Rule: "b", Message: "m", Pos: at(abs("/w/m/a.go"), 10, 1)},
		{Rule: "a", Message: "m", Pos: at(abs("/w/m/a.go"), 9, 50)},
		{Rule: "a", Check: "z", Message: "a", Pos: at(abs("/w/m/a.go"), 10, 1)},
		{Rule: "a", Message: "b", Pos: at(abs("/w/m/a.go"), 10, 0)},
		{Rule: "a", Message: "a", Pos: at(abs("/w/m/a.go"), 10, 1)},
		{Rule: "a", Message: "m", Pos: at(abs("/w/m/a.go"), 10, 2)},
		{Rule: "a", Message: "m", Pos: at(abs("/w/m/b/c.go"), 1, 1)},
		{Rule: "a", Message: "m", Pos: at(abs("/w/m/a.go"), 10, 2)},
	}
	want := []string{
		"../../x.go:1:1 a m",
		"a.go:9:50 a m",
		"a.go:10:1 a a",
		"a.go:10:1 a b",
		"a.go:10:1 a:z a",
		"a.go:10:1 b m",
		"a.go:10:2 a m",
		"a.go:10:10 a m",
		"b/c.go:1:1 a m",
	}
	for seed := range uint64(500) {
		shuffled := slices.Clone(in)
		rand.New(rand.NewPCG(seed, seed)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := keys(process(t, shuffled, finding.Options{WorkDir: root, Tests: true})); !slices.Equal(got, want) {
			t.Fatalf("seed %d: sorted = %q\nwant %q", seed, got, want)
		}
	}
}

func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	identity := []string{"-c", "user.name=gglint", "-c", "user.email=gglint@example.com", "-c", "commit.gpgsign=false"}
	cmd := exec.CommandContext(t.Context(), "git", append(identity, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func committedRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	isolateGit(t)
	repo := t.TempDir()
	for name, content := range files {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), content)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	return repo
}

const fiveLines = "package a\n\nvar one = 1\nvar two = 2\nvar three = 3\n"

func TestProcessNewFromRevKeepsOnlyChangedLinesAndEveryTypecheck(t *testing.T) {
	repo := committedRepo(t, map[string]string{"a.go": fiveLines, "b.go": fiveLines})
	a, b := filepath.Join(repo, "a.go"), filepath.Join(repo, "b.go")
	writeFile(t, a, strings.Replace(fiveLines, "var two = 2", "var two = 22", 1))
	t.Chdir(repo)
	in := []finding.Finding{
		{Rule: "r", Message: "before", Pos: at(a, 3, 1)},
		{Rule: "r", Message: "changed", Pos: at(a, 4, 1)},
		{Rule: "r", Message: "after", Pos: at(a, 5, 1)},
		{Rule: "r", Message: "unchanged file", Pos: at(b, 4, 1)},
		{Rule: finding.TypecheckRule, Message: "typecheck unchanged line", Pos: at(a, 3, 1)},
		{Rule: finding.TypecheckRule, Message: "typecheck unchanged file", Pos: at(b, 1, 1)},
	}
	out := process(t, in, finding.Options{WorkDir: repo, Tests: true, NewFromRev: "HEAD"})
	var messages []string
	for _, o := range out {
		messages = append(messages, o.Message)
	}
	slices.Sort(messages)
	if want := []string{"changed", "typecheck unchanged file", "typecheck unchanged line"}; !slices.Equal(messages, want) {
		t.Fatalf("kept %q, want %q", messages, want)
	}
}

func TestProcessNewFromRevMatchesPrintedPathsBelowRepoRoot(t *testing.T) {
	repo := committedRepo(t, map[string]string{"sub/c.go": fiveLines})
	sub := filepath.Join(repo, "sub")
	c := filepath.Join(sub, "c.go")
	writeFile(t, c, strings.Replace(fiveLines, "var three = 3", "var three = 33", 1))
	t.Chdir(sub)
	in := []finding.Finding{
		{Rule: "r", Message: "changed", Pos: at(c, 5, 1)},
		{Rule: "r", Message: "unchanged", Pos: at(c, 4, 1)},
	}
	out := process(t, in, finding.Options{WorkDir: sub, Tests: true, NewFromRev: "HEAD"})
	if got, want := keys(out), []string{"c.go:5:1 r changed"}; !slices.Equal(got, want) {
		t.Fatalf("kept %q, want %q", got, want)
	}
}

func TestProcessNewFromRevGitFailureIsDiffError(t *testing.T) {
	isolateGit(t)
	cases := map[string]func(t *testing.T) string{
		"not a repository": func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
			return dir
		},
		"unknown revision": func(t *testing.T) string {
			t.Helper()
			return committedRepo(t, map[string]string{"a.go": fiveLines})
		},
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			wd := dir(t)
			t.Chdir(wd)
			in := []finding.Finding{{Rule: finding.TypecheckRule, Message: "m", Pos: at(filepath.Join(wd, "a.go"), 1, 1)}}
			out, err := finding.Process(t.Context(), in, finding.Options{WorkDir: wd, Tests: true, NewFromRev: "no-such-rev"})
			if err == nil || !strings.HasPrefix(err.Error(), "diff: ") || out != nil {
				t.Fatalf("Process = %v, %v; want no findings and a diff: error", out, err)
			}
		})
	}
}
