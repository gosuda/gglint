package driver_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

// scopeFixture builds a tiny module that imports a dependency from the
// module cache: main.go and sub/sub.go live in the main module, while
// sub/sub_windows.go and ignored.go are excluded by build constraints.
// With vendorDep a vendored copy of the dependency is present. The go
// directive, dependency version and sums come from the enclosing module
// so the fixture survives upgrades.
func scopeFixture(t *testing.T, vendorDep bool) string {
	t.Helper()
	root := t.TempDir()
	goMod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read enclosing go.mod: %v", err)
	}
	var version, directive string
	for line := range strings.Lines(strings.TrimSpace(string(goMod))) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "golang.org/x/tools" {
			version = fields[1]
		}
		if len(fields) == 2 && fields[0] == "go" {
			directive = line
		}
	}
	if version == "" || directive == "" {
		t.Fatalf("enclosing go.mod names no golang.org/x/tools version or go directive")
	}
	goSum, err := os.ReadFile(filepath.Join("..", "..", "go.sum"))
	if err != nil {
		t.Fatalf("read enclosing go.sum: %v", err)
	}
	var sums []string
	for line := range strings.Lines(strings.TrimSpace(string(goSum))) {
		if strings.HasPrefix(line, "golang.org/x/tools "+version+" ") ||
			strings.HasPrefix(line, "golang.org/x/tools "+version+"/go.mod ") {
			sums = append(sums, line)
		}
	}
	if len(sums) != 2 {
		t.Fatalf("want source and module sums for golang.org/x/tools %s, got %q", version, sums)
	}
	writeGo(t, root, "go.mod", "module example.com/fixscope\n\n"+directive+"\n\nrequire golang.org/x/tools "+version+"\n")
	writeGo(t, root, "go.sum", strings.Join(sums, "\n")+"\n")
	writeGo(t, root, "main.go", "package main\n\nimport _ \"golang.org/x/tools/go/ast/astutil\"\n\nfunc main() {}\n")
	writeGo(t, root, "sub/sub.go", "package sub\n")
	writeGo(t, root, "sub/sub_windows.go", "//go:build windows\n\npackage sub\n")
	writeGo(t, root, "ignored.go", "//go:build ignore\n\npackage main\n")
	if vendorDep {
		vendor := exec.CommandContext(t.Context(), "go", "mod", "vendor")
		vendor.Dir = root
		vendor.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off")
		if out, err := vendor.CombinedOutput(); err != nil {
			t.Fatalf("go mod vendor: %v\n%s", err, out)
		}
	}
	return root
}

func TestPackageGoFilesExcludesDependencyAndStandardSources(t *testing.T) {
	root := scopeFixture(t, true)
	inv := invocation(root)
	inv.Patterns = []string{"./...", "fmt", "golang.org/x/tools/go/ast/astutil", "example.com/fixscope/vendor/golang.org/x/tools/go/ast/astutil"}
	files, err := driver.PackageGoFiles(t.Context(), inv)
	if err != nil {
		t.Fatalf("PackageGoFiles: %v", err)
	}
	for _, f := range files {
		if !strings.HasPrefix(f, root+string(os.PathSeparator)) {
			t.Errorf("selected %q outside the main module", f)
		}
		for _, elem := range strings.Split(f, string(os.PathSeparator)) {
			if elem == "vendor" {
				t.Errorf("selected vendored source %q", f)
			}
		}
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

func TestApplyRefusesFixOutsideSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "a/a.go", plainMain)
	pre := snapshot(t, dir)
	changed := strings.Replace(plainMain, "return 1", "return 2", 1)
	fixed := map[string][]byte{"a/a.go": []byte(changed), "vendor/dep/dep.go": []byte("package dep\n")}
	violations, err := driver.ApplyFixed(dir, pre, fixed, true)
	if err != nil {
		t.Fatalf("ApplyFixed: %v", err)
	}
	if want := []string{"vendor/dep/dep.go: " + driver.ViolateScope}; !slices.Equal(violationPaths(violations), want) {
		t.Fatalf("violations = %v, want %v", violationPaths(violations), want)
	}
	if got := readFile(t, filepath.Join(dir, "a", "a.go")); got != plainMain {
		t.Fatalf("a project fix escaped the refused batch: %q", got)
	}
}
