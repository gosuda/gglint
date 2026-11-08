package finding_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/finding"
)

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

func TestProcessPrintsWorkDirRelativePaths(t *testing.T) {
	work := filepath.Join(string(filepath.Separator), "w", "repo")
	in := []finding.Finding{
		{Rule: "a", Message: "m", Pos: at(filepath.Join(work, "pkg", "x.go"), 3, 2), End: at(filepath.Join(work, "pkg", "x.go"), 3, 9)},
		{Rule: "a", Message: "m", Pos: at(filepath.Join(string(filepath.Separator), "w", "other", "y.go"), 1, 1)},
	}
	in[1].Related = []finding.Related{{Position: at(filepath.Join(work, "pkg", "x.go"), 1, 0), Message: "r"}}
	out := process(t, in, finding.Options{WorkDir: work, Tests: true})
	paths := []string{out[0].Pos.Path, out[1].Pos.Path}
	if want := []string{"../other/y.go", "pkg/x.go"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if out[0].Related[0].Path != "pkg/x.go" || out[0].Related[0].Column != 1 {
		t.Fatalf("related not normalized: %+v", out[0].Related)
	}
	if out[1].End.Path != "pkg/x.go" {
		t.Fatalf("end path = %q", out[1].End.Path)
	}
	if in[1].Related[0].Path != filepath.Join(work, "pkg", "x.go") {
		t.Fatal("Process mutated its input")
	}
}

func TestProcessDropsTestFilesWhenDisabled(t *testing.T) {
	work := filepath.Join(string(filepath.Separator), "w")
	in := []finding.Finding{
		{Rule: "a", Pos: at(filepath.Join(work, "x_test.go"), 1, 1)},
		{Rule: "typecheck", Pos: at(filepath.Join(work, "y_test.go"), 1, 1)},
		{Rule: "a", Pos: at(filepath.Join(work, "x.go"), 1, 1)},
	}
	if got := process(t, in, finding.Options{WorkDir: work, Tests: false}); len(got) != 1 || got[0].Pos.Path != "x.go" {
		t.Fatalf("tests=false kept %+v", got)
	}
	if got := process(t, in, finding.Options{WorkDir: work, Tests: true}); len(got) != 3 {
		t.Fatalf("tests=true kept %d findings, want 3", len(got))
	}
}

func TestProcessDedupesAndSorts(t *testing.T) {
	work := filepath.Join(string(filepath.Separator), "w")
	f := func(rule, check, file string, line, col int, msg string) finding.Finding {
		return finding.Finding{Rule: rule, Check: check, Message: msg, Pos: at(filepath.Join(work, file), line, col)}
	}
	in := []finding.Finding{
		f("b", "", "b.go", 1, 1, "x"),
		f("a", "z", "a.go", 2, 1, "x"),
		f("a", "", "a.go", 2, 1, "x"),
		f("a", "", "a.go", 1, 5, "x"),
		f("a", "", "a.go", 1, 5, "x"),
		f("a", "", "a.go", 1, 5, "w"),
		f("a", "", "a.go", 1, 0, "x"),
	}
	out := process(t, in, finding.Options{WorkDir: work, Tests: true})
	var got []string
	for _, o := range out {
		got = append(got, strings.Join([]string{o.Pos.Path, o.Token(), o.Message}, "|"))
	}
	want := []string{"a.go|a|x", "a.go|a|w", "a.go|a|x", "a.go|a|x", "a.go|a:z|x", "b.go|b|x"}
	if len(got) != len(want) {
		t.Fatalf("got %d findings %v, want %d", len(got), got, len(want))
	}
	if out[0].Pos.Column != 1 || out[1].Pos.Column != 5 || out[1].Message != "w" {
		t.Fatalf("sort or column normalization wrong: %+v", out)
	}
}

func TestProcessNewFromRev(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if err := exec.CommandContext(t.Context(), "git", "var", "GIT_AUTHOR_IDENT").Run(); err != nil {
		t.Skip("git has no configured identity")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	file := filepath.Join(repo, "a.go")
	if err := os.WriteFile(file, []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	if err := os.WriteFile(file, []byte("package a\n\nvar x = 1\nvar y = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	in := []finding.Finding{
		{Rule: "a", Message: "old", Pos: at(file, 3, 1)},
		{Rule: "a", Message: "new", Pos: at(file, 4, 1)},
		{Rule: "typecheck", Message: "always", Pos: at(file, 3, 1)},
	}
	out := process(t, in, finding.Options{WorkDir: repo, Tests: true, NewFromRev: "HEAD"})
	var messages []string
	for _, o := range out {
		messages = append(messages, o.Message)
	}
	if want := []string{"always", "new"}; !reflect.DeepEqual(messages, want) {
		t.Fatalf("kept %v, want %v", messages, want)
	}
}

func TestProcessNewFromRevGitFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Chdir(t.TempDir())
	_, err := finding.Process(t.Context(), nil, finding.Options{WorkDir: ".", NewFromRev: "no-such-rev"})
	if err == nil || !strings.HasPrefix(err.Error(), "diff: ") {
		t.Fatalf("error = %v, want a diff: prefix", err)
	}
}
