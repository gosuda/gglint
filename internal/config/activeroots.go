package config

import (
	"os"
	"path/filepath"
	"strings"
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
// It returns nil outside any module, so callers fail closed.
func ActiveModuleRoots(cwd string) []string {
	if wd := os.Getenv("GOWORK"); wd == "off" {
		return singleModule(cwd)
	} else if wd != "" {
		return parseWorkspace(wd)
	}
	if dir, ok := discoverWorkspace(cwd); ok {
		roots := parseWorkspace(dir)
		// A workspace root holding its own go.mod is a main module too.
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "go.mod")); err == nil {
			roots = append(roots, filepath.Dir(dir))
		}
		return roots
	}
	return singleModule(cwd)
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

func parseWorkspace(path string) []string {
	data, err := os.ReadFile(path) //nolint:gosec // path is a discovered go.work file or the GOWORK value the go command itself reads
	if err != nil {
		return nil
	}
	var roots []string
	base := filepath.Dir(path)
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		if abs, err := filepath.Abs(filepath.Join(base, p)); err == nil {
			roots = append(roots, abs)
		}
	}
	for _, p := range usePaths(string(data)) {
		add(p)
	}
	return roots
}

// usePaths extracts the member paths from go.work source, handling the
// single-path and parenthesized block forms and skipping comments.
// Quoted paths keep their quotes for the caller to strip.
func usePaths(src string) []string {
	var toks []string
	for line := range strings.Lines(src) {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		toks = append(toks, strings.Fields(line)...)
	}
	var out []string
	for i := 0; i < len(toks); i++ {
		if toks[i] != "use" || i+1 >= len(toks) {
			continue
		}
		if toks[i+1] == "(" {
			i += 2
			for ; i < len(toks) && toks[i] != ")"; i++ {
				out = append(out, toks[i])
			}
			continue
		}
		out = append(out, toks[i+1])
		i++
	}
	return out
}
