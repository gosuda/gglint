package driver_test

import (
	"path/filepath"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

func TestPackageGoFilesAdmitsNamedFileInMainModule(t *testing.T) {
	t.Parallel()
	root := scopeFixture(t, false)
	inv := invocation(root)
	inv.Patterns = []string{"main.go"}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	if len(files) != 1 || files[0] != filepath.Join(root, "main.go") {
		t.Fatalf("selected = %q, want only [main.go]", files)
	}
}

func TestPackageGoFilesSkipsNamedVendoredFile(t *testing.T) {
	t.Parallel()
	root := scopeFixture(t, true)
	inv := invocation(root)
	inv.Patterns = []string{"vendor/golang.org/x/tools/go/ast/astutil/rewrite.go"}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("selected = %q, want none", files)
	}
}

func TestPackageGoFilesSkipsFileBeneathInactiveNestedModule(t *testing.T) {
	t.Parallel()
	root := scopeFixture(t, false)
	writeGo(t, root, "sub/nested/go.mod", "module example.com/nested\n")
	writeGo(t, root, "sub/nested/n.go", fixableFunc)
	inv := invocation(root)
	inv.Patterns = []string{"sub/nested/n.go"}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("selected = %q, want none: the nested module is not active", files)
	}
}

func TestPackageGoFilesAdmitsFileWhenNestedModuleIsActive(t *testing.T) {
	t.Parallel()
	root := scopeFixture(t, false)
	writeGo(t, root, "sub/nested/go.mod", "module example.com/nested\n")
	writeGo(t, root, "sub/nested/n.go", fixableFunc)
	nested := filepath.Join(root, "sub", "nested")
	inv := invocation(nested)
	inv.Patterns = []string{"n.go"}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	if len(files) != 1 || files[0] != filepath.Join(nested, "n.go") {
		t.Fatalf("selected = %q, want only [n.go]", files)
	}
}
