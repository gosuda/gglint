package driver

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// GoSnapshot maps a workdir-relative slash path of a Go source file to the
// bytes seen before a `-fix` run. VerifyFixed compares the snapshot with the
// tree after the fix and refuses every change the fixer was not admitted to
// make, so a refused edit never leaves a partial write behind.
type GoSnapshot map[string][]byte

// SnapshotGoFiles reads every regular Go source file under workDir through a
// root-scoped handle, so a path swapped for a symlink mid-walk cannot escape
// the tree. A read failure aborts the fix before anything runs.
func SnapshotGoFiles(workDir string) (GoSnapshot, error) {
	root, err := os.OpenRoot(workDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	out := GoSnapshot{}
	err = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		return addSnapshotFile(out, root, workDir, path)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func addSnapshotFile(out GoSnapshot, root *os.Root, workDir, path string) error {
	rel, err := filepath.Rel(workDir, path)
	if err != nil {
		return err
	}
	name := filepath.ToSlash(rel)
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := readRootFile(root, name)
	if err != nil {
		return err
	}
	out[name] = data
	return nil
}

func readRootFile(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return data, err
}

// FixViolation is one post-fix change the gate refuses. Pre holds the bytes to
// restore, or nil when the path was created by the fix and must be removed.
type FixViolation struct {
	Path   string
	Reason string
	Pre    []byte
}

// Reasons VerifyFixed reports. Like the vettool refusal reasons they belong to
// the publication contract in the README section "Automatic fixes".
const (
	ViolateCreated = "created during fix"
	ViolateDeleted = "deleted during fix"
	ViolateGen     = "generated file"
	ViolateTests   = "test file excluded when tests are off"
	ViolateParse   = "does not parse after fix"
	ViolateStale   = "changed without an applied fix"
)

// VerifyFixed compares a pre-fix snapshot with the current tree. applied names
// the relative paths the fix run applied; a nil map skips the staleness check
// because the fixer does not account its writes. tests mirrors the run
// configuration: test files are admitted only when tests are enabled.
func VerifyFixed(workDir string, pre GoSnapshot, applied map[string]bool, tests bool) []FixViolation {
	post, err := SnapshotGoFiles(workDir)
	if err != nil {
		return []FixViolation{{Path: ".", Reason: "cannot rescan after fix: " + err.Error()}}
	}
	var out []FixViolation
	for path, before := range pre {
		after, ok := post[path]
		if !ok {
			out = append(out, FixViolation{Path: path, Reason: ViolateDeleted, Pre: before})
			continue
		}
		if slices.Equal(before, after) {
			continue
		}
		if reason, ok := refuseChange(path, before, after, applied, tests); ok {
			out = append(out, FixViolation{Path: path, Reason: reason, Pre: before})
		}
	}
	for path := range post {
		if _, ok := pre[path]; !ok {
			out = append(out, FixViolation{Path: path, Reason: ViolateCreated})
		}
	}
	slices.SortFunc(out, func(a, b FixViolation) int { return strings.Compare(a.Path, b.Path) })
	return out
}

func refuseChange(path string, before, after []byte, applied map[string]bool, tests bool) (string, bool) {
	if generated(before) || generated(after) {
		return ViolateGen, true
	}
	if !tests && strings.HasSuffix(path, "_test.go") {
		return ViolateTests, true
	}
	if parses(before) && !parses(after) {
		return ViolateParse, true
	}
	if applied != nil && !applied[path] {
		return ViolateStale, true
	}
	return "", false
}

func generated(src []byte) bool {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	return err == nil && file != nil && ast.IsGenerated(file)
}

func parses(src []byte) bool {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	return err == nil
}

// Undo restores the pre-fix state: it rewrites the snapshotted bytes, or
// removes a path the fix created.
func (v FixViolation) Undo(workDir string) error {
	path := filepath.Join(workDir, filepath.FromSlash(v.Path))
	if v.Pre == nil {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", v.Path, err)
		}
		return nil
	}
	if err := os.WriteFile(path, v.Pre, 0o600); err != nil {
		return fmt.Errorf("restore %s: %w", v.Path, err)
	}
	return nil
}
