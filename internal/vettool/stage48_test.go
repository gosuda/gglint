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

	"github.com/gosuda/gglint/internal/config"
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
			got, err := stageUnit(unit, t.TempDir(), nil)
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
	// Sequential: staging is verified against the working directory.
	t.Setenv("GOWORK", "")
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
			mod := t.TempDir()
			if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(tc.goMod), 0o600); err != nil {
				t.Fatal(err)
			}
			arg := writeModuleFile(t, mod, "p/p.go")
			t.Chdir(mod)
			got := stageFiles(t, []string{arg})
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
	// Sequential: each case runs with a different working directory, and
	// the scope under test is defined by that directory.
	t.Setenv("GOWORK", "")
	mod := t.TempDir()
	writeGoMod(t, mod, "module example.com/m\n")
	nested := filepath.Join(mod, "sub", "nested")
	writeGoMod(t, nested, "module example.com/nested\n")
	outside := writeModuleFile(t, t.TempDir(), "q.go")
	other := t.TempDir()
	writeGoMod(t, other, "module example.com/o\n")
	ws := t.TempDir()
	if err := os.MkdirAll(ws, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "go.work"), []byte("go 1.27\n\nuse ./a\nuse ./b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"a", "b"} {
		writeGoMod(t, filepath.Join(ws, dir), "module example.com/"+dir+"\n")
	}
	for _, tc := range []struct {
		name       string
		cwd        string
		goFiles    []string
		wantStaged bool
	}{
		{"named file in main module", mod, []string{filepath.Join(mod, "p", "p.go")}, true},
		{"file outside any module", mod, []string{outside}, false},
		{"vendored file", mod, []string{filepath.Join(mod, "vendor", "example.com", "dep", "dep.go")}, false},
		{"no files", mod, nil, false},
		{"files across modules", mod, []string{filepath.Join(mod, "p", "p.go"), filepath.Join(other, "r.go")}, false},
		{"nested module file from parent", mod, []string{filepath.Join(nested, "n.go")}, false},
		{"nested module file from itself", nested, []string{filepath.Join(nested, "n.go")}, true},
		{"workspace member file", filepath.Join(ws, "a"), []string{filepath.Join(ws, "b", "x.go")}, true},
		{"inactive nested file in workspace", filepath.Join(ws, "a"), []string{filepath.Join(ws, "a", "sub", "nested", "n.go")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeModuleFile(t, mod, "p/p.go")
			writeModuleFile(t, nested, "n.go")
			writeModuleFile(t, other, "r.go")
			writeModuleFile(t, filepath.Join(ws, "b"), "x.go")
			writeModuleFile(t, filepath.Join(ws, "a", "sub", "nested"), "n.go")
			writeModuleFile(t, mod, filepath.Join("vendor", "example.com", "dep", "dep.go"))
			writeGoMod(t, filepath.Join(ws, "a", "sub", "nested"), "module example.com/anested\n")
			t.Chdir(tc.cwd)
			got := stageFiles(t, tc.goFiles)
			if tc.wantStaged != (got != "") {
				t.Fatalf("stageUnit = %q, want staged = %v", got, tc.wantStaged)
			}
		})
	}
}

func TestUnitModuleRootRejectsVendorByGuard(t *testing.T) {
	t.Parallel()
	mod := t.TempDir()
	writeGoMod(t, mod, "module example.com/m\n")
	dep := writeModuleFile(t, mod, filepath.Join("vendor", "example.com", "dep", "dep.go"))
	if _, ok := config.EnclosingModule(filepath.Dir(dep)); !ok {
		t.Fatal("the vendor fixture must sit inside the module for the guard to decide")
	}
	if _, ok := unitModuleRoot([]string{dep}, mod, []string{mod}); ok {
		t.Fatal("unitModuleRoot admitted a vendored file")
	}
}

func writeGoMod(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stageFiles(t *testing.T, files []string) string {
	t.Helper()
	return stageFilesWithRoots(t, files, nil)
}

func stageFilesWithRoots(t *testing.T, files, roots []string) string {
	t.Helper()
	cfg, err := json.Marshal(&unitchecker.Config{
		ID:         "command-line-arguments",
		ImportPath: "command-line-arguments",
		GoFiles:    files,
	})
	if err != nil {
		t.Fatal(err)
	}
	unit := writeFile(t, "unit.cfg", cfg)
	got, err := stageUnit(unit, t.TempDir(), roots)
	if err != nil {
		t.Fatalf("stageUnit: %v", err)
	}
	return got
}

func TestStageUnitStagesFileInsideInvocationRoots(t *testing.T) {
	t.Parallel()
	mod := t.TempDir()
	writeGoMod(t, mod, "module example.com/m\n")
	file := writeModuleFile(t, mod, "p/p.go")
	if got := stageFilesWithRoots(t, []string{file}, []string{mod}); got == "" {
		t.Fatal("stageUnit skipped a file inside the invocation roots")
	}
}
