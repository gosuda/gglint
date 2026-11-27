package config

import (
	"os"
	"path/filepath"
	"strings"
)

// EnclosingModule walks up from dir to find the nearest ancestor holding
// go.mod, and reports that module root. It reports false when no ancestor
// holds one, so callers fail closed outside any module.
func EnclosingModule(dir string) (string, bool) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", false
		}
		for _, entry := range entries {
			if entry.Name() == "go.mod" {
				return current, true
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// InModuleVendor reports whether dir sits under a vendor directory inside
// the module rooted at moduleDir. Only components below the module root
// count: a checkout kept under a directory named vendor is not itself
// vendored. An empty module root or a dir outside it fails closed.
func InModuleVendor(moduleDir, dir string) bool {
	if moduleDir == "" {
		return true
	}
	rel, err := filepath.Rel(moduleDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "vendor" {
			return true
		}
	}
	return false
}
