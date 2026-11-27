package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func writeConfigFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSameDirMatchesAcrossSymlinks(t *testing.T) {
	t.Parallel()
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !config.SameDir(target, link) {
		t.Fatalf("SameDir(%q, %q) = false through a symlink", target, link)
	}
	if !config.ContainsRoot([]string{target}, link) {
		t.Fatalf("ContainsRoot misses a symlinked member")
	}
	if config.SameDir(target, filepath.Join(target, "other")) {
		t.Fatalf("SameDir matches distinct directories")
	}
}

func TestActiveModuleRootsCoversWorkspaceMembers(t *testing.T) {
	t.Setenv("GOWORK", "")
	root := t.TempDir()
	writeConfigFile(t, root, "go.work", "go 1.27\n\nuse ./a\nuse (\n\t./b // second\n)\n")
	writeConfigFile(t, root, "a/go.mod", "module example.com/a\n")
	writeConfigFile(t, root, "b/go.mod", "module example.com/b\n")
	writeConfigFile(t, root, "a/pkg/keep", "")
	writeConfigFile(t, root, "b/pkg/keep", "")
	got := config.ActiveModuleRoots(filepath.Join(root, "a", "pkg"))
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "b")}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("ActiveModuleRoots = %q, want %q", got, want)
	}
	if roots := config.ActiveModuleRoots(filepath.Join(root, "a")); len(roots) == 0 {
		t.Fatalf("ActiveModuleRoots misses a workspace from its own member")
	}
}

func TestActiveModuleRootsFallsBackToSingleModule(t *testing.T) {
	t.Setenv("GOWORK", "")
	mod := t.TempDir()
	writeConfigFile(t, mod, "go.mod", "module example.com/solo\n")
	writeConfigFile(t, mod, "pkg/keep", "")
	got := config.ActiveModuleRoots(filepath.Join(mod, "pkg"))
	if len(got) != 1 {
		t.Fatalf("ActiveModuleRoots = %q, want the single module", got)
	}
	want, err := filepath.EvalSymlinks(mod)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != want {
		t.Fatalf("ActiveModuleRoots = %q, want %q", got, want)
	}
	if roots := config.ActiveModuleRoots(t.TempDir()); roots != nil {
		t.Fatalf("ActiveModuleRoots outside any module = %q, want nil", roots)
	}
}

func TestActiveModuleRootsHonorsGoWorkOff(t *testing.T) {
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	writeConfigFile(t, root, "go.work", "go 1.27\n\nuse ./a\n")
	writeConfigFile(t, root, "a/go.mod", "module example.com/a\n")
	got := config.ActiveModuleRoots(filepath.Join(root, "a"))
	if len(got) != 1 {
		t.Fatalf("GOWORK=off ActiveModuleRoots = %q, want the single module", got)
	}
}

func TestActiveModuleRootsTreatsAutoLikeUnset(t *testing.T) {
	t.Setenv("GOWORK", "auto")
	mod := t.TempDir()
	writeConfigFile(t, mod, "go.mod", "module example.com/solo\n")
	got := config.ActiveModuleRoots(mod)
	if len(got) != 1 || got[0] != mod {
		t.Fatalf("GOWORK=auto ActiveModuleRoots = %q, want [%q]", got, mod)
	}
	root := t.TempDir()
	writeConfigFile(t, root, "go.work", "go 1.27\n\nuse ./a\n")
	writeConfigFile(t, root, "a/go.mod", "module example.com/a\n")
	got = config.ActiveModuleRoots(filepath.Join(root, "a"))
	if len(got) != 1 || got[0] != filepath.Join(root, "a") {
		t.Fatalf("GOWORK=auto ActiveModuleRoots = %q, want the workspace member", got)
	}
}

func TestActiveModuleRootsSkipsUnlistedWorkspaceRoot(t *testing.T) {
	t.Setenv("GOWORK", "")
	root := t.TempDir()
	writeConfigFile(t, root, "go.work", "go 1.27\n\nuse ./a\n")
	writeConfigFile(t, root, "go.mod", "module example.com/root\n")
	writeConfigFile(t, root, "a/go.mod", "module example.com/a\n")
	got := config.ActiveModuleRoots(root)
	want := []string{filepath.Join(root, "a")}
	if !slices.Equal(got, want) {
		t.Fatalf("ActiveModuleRoots = %q, want %q: the unlisted root module is not a member", got, want)
	}
}

func TestActiveModuleRootsResolvesAbsoluteAndQuotedMembers(t *testing.T) {
	t.Setenv("GOWORK", "")
	root := t.TempDir()
	outside := t.TempDir()
	writeConfigFile(t, outside, "go.mod", "module example.com/outside\n")
	writeConfigFile(t, root, "my app/go.mod", "module example.com/spaced\n")
	writeConfigFile(t, root, "go.work", "go 1.27\n\nuse "+outside+"\nuse \"./my app\"\n")
	got := config.ActiveModuleRoots(root)
	if !slices.Contains(got, outside) {
		t.Fatalf("ActiveModuleRoots = %q, want it to contain absolute member %q", got, outside)
	}
	if !slices.Contains(got, filepath.Join(root, "my app")) {
		t.Fatalf("ActiveModuleRoots = %q, want it to contain the quoted member", got)
	}
}
