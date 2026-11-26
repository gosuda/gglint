package vettool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"
)

// stdFile names a file inside the standard library without the deprecated
// runtime.GOROOT: the toolchain running the test reports its own root.
func stdFile(t *testing.T, rel string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	return filepath.Join(strings.TrimSpace(string(out)), rel)
}

func TestStageUnitSkipsUnitsOutsideMainModule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		module     *analysis.Module
		goFiles    []string
		wantStaged bool
	}{
		{"standard package", nil, []string{stdFile(t, filepath.Join("src", "fmt", "doc.go"))}, false},
		{"dependency", &analysis.Module{Path: "example.com/dep"}, []string{"p.go"}, false},
		{"main module", &analysis.Module{Path: "example.com/m", Main: true}, []string{"p.go"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := json.Marshal(&unitchecker.Config{
				ID:         "example.com/m/p",
				ImportPath: "example.com/m/p",
				GoFiles:    tc.goFiles,
				Module:     tc.module,
			})
			if err != nil {
				t.Fatal(err)
			}
			unit := writeFile(t, "unit.cfg", cfg)
			got, err := stageUnit(unit, t.TempDir())
			if err != nil {
				t.Fatalf("stageUnit: %v", err)
			}
			if tc.wantStaged != (got != "") {
				t.Fatalf("stageUnit = %q, want staged = %v", got, tc.wantStaged)
			}
		})
	}
}

func TestStageUnitAcceptsWhitespaceSeparatedModuleDirective(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		goMod      string
		wantStaged bool
	}{
		{"space separated", "module example.com/m\n", true},
		{"tab separated", "module\texample.com/m\n", true},
		{"padded with comment", "  module   example.com/m   // local\n", true},
		{"tab separated std stays skipped", "module\tstd\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mod := t.TempDir()
			if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(tc.goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			arg := writeModuleFile(t, mod, "p/p.go")
			cfg, err := json.Marshal(&unitchecker.Config{
				ID:         "command-line-arguments",
				ImportPath: "command-line-arguments",
				GoFiles:    []string{arg},
			})
			if err != nil {
				t.Fatal(err)
			}
			unit := writeFile(t, "unit.cfg", cfg)
			got, err := stageUnit(unit, t.TempDir())
			if err != nil {
				t.Fatalf("stageUnit: %v", err)
			}
			if tc.wantStaged != (got != "") {
				t.Fatalf("stageUnit = %q, want staged = %v", got, tc.wantStaged)
			}
		})
	}
}

func writeModuleFile(t *testing.T, dir, rel string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStageUnitVerifiesMetadataLessUnits(t *testing.T) {
	t.Parallel()
	mod := t.TempDir()
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inModule := writeModuleFile(t, mod, "p/p.go")
	vendored := writeModuleFile(t, mod, "vendor/example.com/dep/dep.go")
	outside := writeModuleFile(t, t.TempDir(), "q.go")
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "go.mod"), []byte("module example.com/o\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherFile := writeModuleFile(t, other, "r.go")
	for _, tc := range []struct {
		name       string
		goFiles    []string
		wantStaged bool
	}{
		{"named file in main module", []string{inModule}, true},
		{"file outside any module", []string{outside}, false},
		{"vendored file", []string{vendored}, false},
		{"no files", nil, false},
		{"files across modules", []string{inModule, otherFile}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := json.Marshal(&unitchecker.Config{
				ID:         "command-line-arguments",
				ImportPath: "command-line-arguments",
				GoFiles:    tc.goFiles,
			})
			if err != nil {
				t.Fatal(err)
			}
			unit := writeFile(t, "unit.cfg", cfg)
			got, err := stageUnit(unit, t.TempDir())
			if err != nil {
				t.Fatalf("stageUnit: %v", err)
			}
			if tc.wantStaged != (got != "") {
				t.Fatalf("stageUnit = %q, want staged = %v", got, tc.wantStaged)
			}
		})
	}
}
