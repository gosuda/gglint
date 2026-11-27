package driver_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

const fixableFunc = "package p\n\nfunc F() int {\n\n\treturn 1\n}\n"

// workspaceFixture builds a two-module workspace: root holds only go.work,
// module a has alpha.go and module b has beta.go, both fixable.
func workspaceFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	writeGo(t, root, "go.work", "go 1.27\n\nuse ./a\nuse ./b\n")
	writeGo(t, root, "a/go.mod", "module example.com/a\n")
	writeGo(t, root, "a/alpha.go", fixableFunc)
	writeGo(t, root, "b/go.mod", "module example.com/b\n")
	writeGo(t, root, "b/beta.go", fixableFunc)
	return root
}

func TestPackageGoFilesAdmitsNamedFilesAcrossWorkspace(t *testing.T) {
	t.Parallel()
	root := workspaceFixture(t)
	for _, want := range []string{filepath.Join("a", "alpha.go"), filepath.Join("b", "beta.go")} {
		inv := invocation(root)
		// go list accepts named files from a single directory per
		// call, so each module file is resolved on its own.
		inv.Patterns = []string{want}
		files, err := driver.PackageGoFiles(t.Context(), inv)
		if err != nil {
			t.Fatalf("PackageGoFiles(%s): %v", want, err)
		}
		if len(files) != 1 || files[0] != filepath.Join(root, want) {
			t.Fatalf("selected(%s) = %q, want only itself", want, files)
		}
	}
}

func TestPackageGoFilesSkipsFileOutsideWorkspace(t *testing.T) {
	t.Parallel()
	root := workspaceFixture(t)
	writeGo(t, root, "notes.go", fixableFunc)
	inv := invocation(root)
	inv.Patterns = []string{"notes.go"}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("selected = %q, want none", files)
	}
}

func TestWorkspaceMemberWithoutGoWorkEntryIsSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGo(t, root, "go.work", "go 1.27\n\nuse ./a\n")
	writeGo(t, root, "a/go.mod", "module example.com/a\n")
	writeGo(t, root, "a/alpha.go", fixableFunc)
	writeGo(t, root, "b/go.mod", "module example.com/b\n")
	writeGo(t, root, "b/beta.go", fixableFunc)
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		{"a/alpha.go", []string{filepath.Join(root, "a", "alpha.go")}},
		{"b/beta.go", nil},
	} {
		inv := invocation(root)
		inv.Patterns = []string{tc.pattern}
		files, err := driver.PackageGoFiles(t.Context(), inv)
		if err != nil {
			t.Fatalf("PackageGoFiles(%s): %v", tc.pattern, err)
		}
		if !slices.Equal(files, tc.want) {
			t.Fatalf("selected(%s) = %q, want %q", tc.pattern, files, tc.want)
		}
	}
}

func TestPackageGoFilesSkipsUnlistedWorkspaceRootModule(t *testing.T) {
	// Sequential: the test pins GOWORK so workspace discovery is hermetic.
	t.Setenv("GOWORK", "")
	root := t.TempDir()
	writeGo(t, root, "go.work", "go 1.27\n\nuse ./a\n")
	writeGo(t, root, "go.mod", "module example.com/root\n")
	writeGo(t, root, "root.go", fixableFunc)
	writeGo(t, root, "a/go.mod", "module example.com/a\n")
	writeGo(t, root, "a/alpha.go", fixableFunc)
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		{"a/alpha.go", []string{filepath.Join(root, "a", "alpha.go")}},
		{"root.go", nil},
	} {
		inv := invocation(root)
		inv.Patterns = []string{tc.pattern}
		files, err := driver.PackageGoFiles(t.Context(), inv)
		if err != nil {
			t.Fatalf("PackageGoFiles(%s): %v", tc.pattern, err)
		}
		if !slices.Equal(files, tc.want) {
			t.Fatalf("selected(%s) = %q, want %q", tc.pattern, files, tc.want)
		}
	}
}
