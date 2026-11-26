package driver_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

func TestPackageGoFilesIncludesBuildExcludedSources(t *testing.T) {
	t.Parallel()
	root := scopeFixture(t, false)
	inv := invocation(root)
	inv.Patterns = []string{"./..."}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	for _, want := range []string{filepath.Join("sub", "sub_windows.go"), "ignored.go"} {
		if !slices.Contains(files, filepath.Join(root, want)) {
			t.Errorf("selection %q misses build-excluded source %q", files, want)
		}
	}
}

func TestVerifyRefusesChangeToBuildExcludedSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "a/a.go", plainMain)
	writeGo(t, dir, "a/a_windows.go", "//go:build windows\n\n"+plainMain)
	pre := snapshot(t, dir)
	writeGo(t, dir, "a/a_windows.go", "//go:build windows\n\n"+strings.Replace(plainMain, "return 1", "return 2", 1))
	got := driver.VerifyFixed(dir, pre, map[string]bool{}, true)
	if want := []string{"a/a_windows.go: " + driver.ViolateStale}; !slices.Equal(violationPaths(got), want) {
		t.Fatalf("violations = %v, want %v", violationPaths(got), want)
	}
}
