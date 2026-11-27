package driver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

func TestFixUsesAbsoluteStagingDirectory(t *testing.T) {
	dir := installGo(t, goRun{})
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	work := t.TempDir()
	t.Chdir(work)
	if err := os.Mkdir("rel", 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", "rel")
	if _, err := driver.Fix(t.Context(), invocation(work)); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(readFile(t, filepath.Join(dir, "args")), "\n"), "\n")
	var staged string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "-fix-output="); ok {
			staged = v
		}
	}
	if staged == "" {
		t.Fatal("go vet ran without -fix-output")
	}
	if !filepath.IsAbs(staged) {
		t.Fatalf("-fix-output = %q with a relative TMPDIR, want an absolute directory", staged)
	}
}
