package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

const levelCount = 4

type discoverCase struct {
	configs int
	git     int
	gitKind string
}

func discoverCases() []discoverCase {
	var cases []discoverCase
	for configs := 1; configs < 1<<levelCount; configs++ {
		cases = append(cases, discoverCase{configs: configs, git: -1})
	}
	for configs := range 1 << levelCount {
		for git := range levelCount {
			cases = append(cases, discoverCase{configs, git, "dir"}, discoverCase{configs, git, "file"})
		}
	}
	return cases
}

func TestDiscoverReturnsNearestConfigUpToAndIncludingGitDir(t *testing.T) {
	for _, c := range discoverCases() {
		t.Run(fmt.Sprintf("configs=%04b/git=%d/%s", c.configs, c.git, c.gitKind), func(t *testing.T) {
			root := t.TempDir()
			levels := []string{filepath.Join(root, "a", "b", "c"), filepath.Join(root, "a", "b"), filepath.Join(root, "a"), root}
			want := buildLevels(t, levels, c)
			got, err := config.Discover(levels[0])
			if err != nil || got != want {
				t.Fatalf("Discover = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func buildLevels(t *testing.T, levels []string, c discoverCase) string {
	t.Helper()
	if err := os.MkdirAll(levels[0], 0o755); err != nil {
		t.Fatal(err)
	}
	want := ""
	for i, level := range slices.Backward(levels) {
		if i == c.git {
			want = ""
			markGit(t, level, c.gitKind)
		}
		if c.configs&(1<<i) != 0 {
			want = writeFile(t, level, config.FileName, "")
		}
	}
	return want
}

func markGit(t *testing.T, dir, kind string) {
	t.Helper()
	if kind == "file" {
		writeFile(t, dir, ".git", "gitdir: elsewhere\n")
		return
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverIgnoresConfigsBelowAndBesideWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	markGit(t, root, "dir")
	wd := filepath.Join(root, "a")
	writeFile(t, filepath.Join(wd, "child"), config.FileName, "")
	writeFile(t, filepath.Join(root, "sibling"), config.FileName, "")
	got, err := config.Discover(wd)
	if err != nil || got != "" {
		t.Fatalf("Discover = %q, %v; want none", got, err)
	}
}
