package driver_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

func TestPackageGoFilesKeepsModuleUnderVendorPath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "vendor", "proj")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	writeScopeFixture(t, root, true)
	inv := invocation(root)
	inv.Patterns = []string{"./..."}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	var got []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, rel)
	}
	want := []string{"ignored.go", "main.go", filepath.Join("sub", "sub.go"), filepath.Join("sub", "sub_windows.go")}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("selected = %q, want %q", got, want)
	}
}
