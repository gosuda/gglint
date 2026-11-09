//go:build unix

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

func TestDiscoverStopsAtSymlinkToWorldWritableDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, config.FileName, "")
	shared := filepath.Join(root, "real", "shared")
	makeWorldWritable(t, shared)
	link := filepath.Join(root, "link")
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	got, err := config.Discover(link)
	if err != nil || got != "" {
		t.Fatalf("Discover = %q, %v; want none", got, err)
	}
}
