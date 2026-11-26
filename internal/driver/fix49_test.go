package driver_test

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

func TestSnapshotSkipsSymlinkedSources(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	dir := t.TempDir()
	writeGo(t, dir, "a/a.go", plainMain)
	writeGo(t, dir, "shared.go", plainMain)
	link := filepath.Join(dir, "a", "link.go")
	if err := os.Symlink(filepath.Join(dir, "shared.go"), link); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, dir)
	if len(snap) != 1 || snap["a/a.go"].Bytes == nil {
		t.Fatalf("snapshot keys = %v, want only [a/a.go]", slices.Sorted(maps.Keys(snap)))
	}
	// A dangling symlink is skipped the same way instead of aborting the run.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "absent.go"), link); err != nil {
		t.Fatal(err)
	}
	snap = snapshot(t, dir)
	if len(snap) != 1 || snap["a/a.go"].Bytes == nil {
		t.Fatalf("snapshot keys = %v, want only [a/a.go]", slices.Sorted(maps.Keys(snap)))
	}
}

func TestApplyRefusesFixForSkippedSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	dir := t.TempDir()
	writeGo(t, dir, "a/a.go", plainMain)
	writeGo(t, dir, "shared.go", plainMain)
	if err := os.Symlink(filepath.Join(dir, "shared.go"), filepath.Join(dir, "a", "link.go")); err != nil {
		t.Fatal(err)
	}
	pre, err := driver.SnapshotGoFiles(dir, filepath.Join(dir, "a", "a.go"), filepath.Join(dir, "a", "link.go"))
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	t.Cleanup(func() {
		if err := pre.Close(); err != nil {
			t.Error(err)
		}
	})
	changed := strings.Replace(plainMain, "return 1", "return 2", 1)
	violations, err := driver.ApplyFixed(dir, pre, map[string][]byte{"a/link.go": []byte(changed)}, true)
	if err != nil {
		t.Fatalf("ApplyFixed: %v", err)
	}
	if want := []string{"a/link.go: " + driver.ViolateScope}; !slices.Equal(violationPaths(violations), want) {
		t.Fatalf("violations = %v, want %v", violationPaths(violations), want)
	}
	if got := readFile(t, filepath.Join(dir, "a", "a.go")); got != plainMain {
		t.Fatalf("selected source changed: %q", got)
	}
}
