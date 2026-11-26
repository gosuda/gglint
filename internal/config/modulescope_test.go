package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func TestEnclosingModuleFindsNearestGoMod(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	got, ok := config.EnclosingModule(nested)
	if !ok {
		t.Fatalf("EnclosingModule(%q) failed inside a module", nested)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("EnclosingModule(%q) = %q, want %q", nested, got, want)
	}
}

func TestEnclosingModuleFailsClosedOutsideModules(t *testing.T) {
	t.Parallel()
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := config.EnclosingModule(outside); ok {
		t.Fatalf("EnclosingModule(%q) succeeded outside any module", outside)
	}
}

func TestInModuleVendorScopesToModuleRoot(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	root := sep + filepath.Join("home", "u", "vendor", "app")
	for _, tc := range []struct {
		name      string
		moduleDir string
		dir       string
		want      bool
	}{
		{"module under vendor path", root, filepath.Join(root, "pkg"), false},
		{"vendored copy", sep + "m", filepath.Join(sep+"m", "vendor", "example.com", "dep"), true},
		{"ordinary package", sep + "m", filepath.Join(sep+"m", "pkg"), false},
		{"module root itself", root, root, false},
		{"outside the module", sep + "m", filepath.Join(sep + "other"), true},
		{"empty module root", "", filepath.Join(sep+"m", "pkg"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := config.InModuleVendor(tc.moduleDir, tc.dir); got != tc.want {
				t.Fatalf("InModuleVendor(%q, %q) = %v, want %v", tc.moduleDir, tc.dir, got, tc.want)
			}
		})
	}
}
