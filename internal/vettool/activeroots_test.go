package vettool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestActiveRootsMissingManifestFallsBackToDiscovery(t *testing.T) {
	t.Parallel()
	if got := activeRoots(t.TempDir()); got != nil {
		t.Fatalf("activeRoots without manifest = %q, want nil for discovery", got)
	}
}

func TestActiveRootsExplicitEmptyFailsClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, activeRootsFile), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := activeRoots(dir)
	if got == nil {
		t.Fatal("manifest [] decoded to nil, want an explicitly empty list")
	}
	if len(got) != 0 {
		t.Fatalf("manifest [] decoded to %q, want empty", got)
	}
}

func TestActiveRootsCorruptManifestFailsClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, activeRootsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := activeRoots(dir)
	if got == nil {
		t.Fatal("corrupt manifest decoded to nil, want an explicitly empty list")
	}
	if len(got) != 0 {
		t.Fatalf("corrupt manifest decoded to %q, want empty", got)
	}
}

func TestActiveRootsRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := []string{filepath.Join("a", "alpha"), filepath.Join("b", "beta")}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, activeRootsFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := activeRoots(dir); !slices.Equal(got, want) {
		t.Fatalf("activeRoots = %q, want %q", got, want)
	}
}

func TestStageUnitRefusesExplicitEmptyScope(t *testing.T) {
	// Sequential: discovery fallback and staging are defined by the working
	// directory, and GOWORK is process-wide.
	t.Setenv("GOWORK", "")
	mod := t.TempDir()
	writeGoMod(t, mod, "module example.com/m\n")
	file := writeModuleFile(t, mod, "p/p.go")
	t.Chdir(mod)
	if got := stageFilesWithRoots(t, []string{file}, nil); got == "" {
		t.Fatal("stageUnit with discovery skipped a file inside the module")
	}
	if got := stageFilesWithRoots(t, []string{file}, []string{}); got != "" {
		t.Fatalf("stageUnit with an explicitly empty scope staged %q", got)
	}
}
