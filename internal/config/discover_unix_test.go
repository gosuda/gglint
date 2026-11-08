//go:build unix

package config_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func foreignWritableTempDir(t *testing.T) string {
	t.Helper()
	dir := os.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Skipf("stat %s: %v", dir, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode().Perm()&0o002 == 0 || stat.Uid == uint32(os.Getuid()) {
		t.Skipf("%s is not world-writable and owned by another user", dir)
	}
	return dir
}

func TestDiscoverStopsAtSymlinkToWorldWritableDirOwnedByAnotherUser(t *testing.T) {
	shared := foreignWritableTempDir(t)
	root := t.TempDir()
	writeFile(t, root, config.FileName, "")
	link := filepath.Join(root, "link")
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	got, err := config.Discover(link)
	if err != nil || got != "" {
		t.Fatalf("Discover = %q, %v; want none", got, err)
	}
}
