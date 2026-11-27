package config

import (
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// SameDir reports whether a and b name the same directory, resolving
// symlinks on both sides first. Paths that cannot be resolved compare
// by spelling, so an unresolvable path never matches.
func SameDir(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

// ContainsRoot reports whether dir names one of roots, comparing
// canonically so symlinked checkouts still match.
func ContainsRoot(roots []string, dir string) bool {
	for _, root := range roots {
		if SameDir(root, dir) {
			return true
		}
	}
	return false
}

// ActiveModuleRoots returns the main module roots governing cwd: every
// workspace member when cwd sits in a workspace, else cwd's own module.
// It reports nil outside any module, so callers fail closed.
func ActiveModuleRoots(cwd string) []string {
	switch wd := os.Getenv("GOWORK"); wd {
	case "off":
		return singleModule(cwd)
	case "", "auto":
		// An empty value and "auto" both select workspace discovery.
		if dir, ok := discoverWorkspace(cwd); ok {
			return parseWorkspace(dir)
		}
		return singleModule(cwd)
	default:
		// A relative GOWORK resolves against the invoking directory,
		// matching the go command run there.
		if !filepath.IsAbs(wd) {
			wd = filepath.Join(cwd, wd)
		}
		return parseWorkspace(wd)
	}
}

// singleModule returns cwd's enclosing module, or nil outside any module.
func singleModule(cwd string) []string {
	if root, ok := EnclosingModule(cwd); ok {
		return []string{root}
	}
	return nil
}

// discoverWorkspace walks up from cwd looking for go.work and reports its
// path. It reports false when no ancestor holds one.
func discoverWorkspace(cwd string) (string, bool) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", false
	}
	current := abs
	for {
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", false
		}
		for _, entry := range entries {
			if entry.Name() == "go.work" {
				return filepath.Join(current, "go.work"), true
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// parseWorkspace reports the member directories of a workspace file.
func parseWorkspace(path string) []string {
	data, err := os.ReadFile(path) //nolint:gosec // path is a discovered go.work file or the GOWORK value the go command itself reads
	if err != nil {
		return nil
	}
	// The go.work grammar owns quoting and comments; only keep members the
	// go command itself would load. Absolute members stay absolute.
	work, err := modfile.ParseWork(path, data, nil)
	if err != nil {
		return nil
	}
	base := filepath.Dir(path)
	seen := map[string]bool{}
	var roots []string
	for _, use := range work.Use {
		member := use.Path
		if !filepath.IsAbs(member) {
			member = filepath.Join(base, member)
		}
		abs, err := filepath.Abs(member)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		roots = append(roots, abs)
	}
	return roots
}
