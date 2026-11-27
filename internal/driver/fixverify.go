package driver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gosuda/gglint/internal/config"
)

// GoSnapshot owns the source bytes and open package directories until Close.
type GoSnapshot map[string]SnapshotFile

type SnapshotFile struct {
	Bytes     []byte
	name      string
	mode      fs.FileMode
	directory *snapshotDirectory
}

type snapshotDirectory struct {
	root  *os.Root
	path  string
	names map[string]bool
}

// PackageGoFiles resolves only the requested packages, including their test
// sources so a tests-off run can verify that those files remain unchanged.
func PackageGoFiles(ctx context.Context, inv Invocation) ([]string, error) {
	args := []string{"list", "-e", "-json"}
	if len(inv.BuildTags) > 0 {
		args = append(args, "-tags="+strings.Join(inv.BuildTags, ","))
	}
	args = append(args, inv.Patterns...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = inv.WorkDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("list fix packages: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	dec := jsontext.NewDecoder(&stdout)
	var files []string
	var scope scopeCache
	for {
		var pkg packageRecord
		if err := json.UnmarshalDecode(dec, &pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode fix packages: %w", err)
		}
		files = scope.selectPackage(inv, files, pkg)
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// appendPackageFiles joins the selected file groups of one package into
// the snapshot list.
func appendPackageFiles(files []string, dir string, groups ...[]string) []string {
	for _, group := range groups {
		for _, name := range group {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files
}

// scopeCache remembers the active main modules across the packages of one
// selection, so synthetic file packages resolve them only once.
type scopeCache struct {
	roots   []string
	checked bool
}

// selectPackage appends the snapshot files of one listed package, or
// nothing when the package sits outside the writable scope.
func (s *scopeCache) selectPackage(inv Invocation, files []string, pkg packageRecord) []string {
	if pkg.Dir == "" || pkg.Standard {
		return files
	}
	if _, ok := s.moduleDir(inv.WorkDir, pkg.Module, pkg.Dir); !ok {
		return files
	}
	// IgnoredGoFiles are existing sources the build excludes for this
	// configuration. They are snapshotted so verification still refuses
	// any change to them, even though no staged fix can target them.
	return appendPackageFiles(files, pkg.Dir, pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles, pkg.IgnoredGoFiles)
}

// packageRecord is one go list -json package description.
type packageRecord struct {
	Dir                                          string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
	IgnoredGoFiles                               []string
	// Standard packages and dependencies outside the main module
	// resolve here too, but staged fixes never leave the main
	// module: their files stay out of the writable snapshot, so
	// publication refuses any fix archived for them as outside
	// the analysis scope.
	Standard bool
	Module   *packageModule
}

// packageModule carries the owning module of one listed package.
type packageModule struct {
	Main bool
	Dir  string
}

// moduleDir returns the module directory governing a package for snapshots:
// the package's own main module, or the active main module for synthetic
// file packages without metadata. It reports false for anything outside.
func (s *scopeCache) moduleDir(workDir string, module *packageModule, pkgDir string) (string, bool) {
	if module != nil {
		if !module.Main {
			return "", false
		}
		if config.InModuleVendor(module.Dir, pkgDir) {
			return "", false
		}
		return module.Dir, true
	}
	// Synthetic file packages carry no module metadata: verify their
	// directory against the active main modules instead of skipping
	// their fixes silently. A single newline-joined path would admit
	// nothing; the workspace members are checked one by one.
	if !s.checked {
		s.checked = true
		s.roots = config.ActiveModuleRoots(workDir)
	}
	root, ok := config.EnclosingModule(pkgDir)
	if !ok || !config.ContainsRoot(s.roots, root) {
		return "", false
	}
	if config.InModuleVendor(root, pkgDir) {
		return "", false
	}
	return root, true
}

// SnapshotGoFiles opens only the selected source files. Each package directory
// stays open, so later reads and restores cannot follow a substituted parent.
func SnapshotGoFiles(workDir string, filenames ...string) (GoSnapshot, error) {
	if info, err := os.Stat(workDir); err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, fmt.Errorf("source workdir is not a directory: %s", workDir)
	}
	out := GoSnapshot{}
	directories := map[string]*snapshotDirectory{}
	complete := false
	defer func() {
		if !complete {
			for _, directory := range directories {
				_ = directory.root.Close()
			}
		}
	}()
	for _, filename := range filenames {
		if !strings.HasSuffix(filename, ".go") {
			continue
		}
		if err := snapshotOne(out, directories, workDir, filename); err != nil {
			return nil, err
		}
	}
	complete = true
	return out, nil
}

func (s GoSnapshot) Close() error {
	var failures []error
	for _, directory := range s.directories() {
		if err := directory.root.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s GoSnapshot) directories() []*snapshotDirectory {
	seen := map[*snapshotDirectory]bool{}
	var out []*snapshotDirectory
	for _, file := range s {
		if !seen[file.directory] {
			seen[file.directory] = true
			out = append(out, file.directory)
		}
	}
	slices.SortFunc(out, func(a, b *snapshotDirectory) int { return strings.Compare(a.path, b.path) })
	return out
}

func rootNames(root *os.Root) (map[string]bool, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(-1)
	if closeErr := dir.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	return names, nil
}

func readRootFile(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err == nil {
		var current fs.FileInfo
		current, err = root.Lstat(name)
		if err == nil && (!current.Mode().IsRegular() || !os.SameFile(opened, current)) {
			err = fmt.Errorf("source changed while opening %s", name)
		}
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(f)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return data, err
}

type FixViolation struct {
	Path   string
	Reason string
	Pre    []byte
	file   SnapshotFile
}

const (
	ViolateCreated = "created during fix"
	ViolateDeleted = "deleted during fix"
	ViolateGen     = "generated file"
	ViolateTests   = "test file excluded when tests are off"
	ViolateParse   = "does not parse after fix"
	ViolateStale   = "changed without an applied fix"
	ViolateScope   = "outside the analysis scope"
)

// VerifyFixed accounts for every selected source change against actual archive
// members. Directory handles, file names and initial bytes come from the snapshot.
func VerifyFixed(workDir string, pre GoSnapshot, applied map[string]bool, tests bool) []FixViolation {
	var out []FixViolation
	for path, before := range pre {
		if reason := verifyCurrent(path, before, applied, tests); reason != "" {
			out = append(out, FixViolation{Path: path, Pre: before.Bytes, file: before, Reason: reason})
		}
	}
	for _, directory := range pre.directories() {
		out = append(out, findCreated(workDir, directory)...)
	}
	slices.SortFunc(out, func(a, b FixViolation) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// verifyCurrent reports how one snapshotted file changed, or empty when it
// did not. Early returns keep each decision flat.
func verifyCurrent(path string, before SnapshotFile, applied map[string]bool, tests bool) string {
	info, err := before.directory.root.Lstat(before.name)
	if errors.Is(err, os.ErrNotExist) {
		return ViolateDeleted
	}
	if err != nil {
		return "cannot inspect after fix: " + err.Error()
	}
	if !info.Mode().IsRegular() {
		return ViolateStale
	}
	after, err := readRootFile(before.directory.root, before.name)
	if err != nil {
		return "cannot read after fix: " + err.Error()
	}
	if bytes.Equal(before.Bytes, after) {
		return ""
	}
	reason, _ := refuseChange(path, before.Bytes, after, applied, tests)
	return reason
}

// findCreated reports go files that appeared in directory after the snapshot.
func findCreated(workDir string, directory *snapshotDirectory) []FixViolation {
	names, err := rootNames(directory.root)
	if err != nil {
		return []FixViolation{{Path: directory.path, Reason: "cannot inspect after fix: " + err.Error()}}
	}
	var out []FixViolation
	for name := range names {
		if directory.names[name] || !strings.HasSuffix(name, ".go") {
			continue
		}
		rel, err := filepath.Rel(workDir, filepath.Join(directory.path, name))
		if err != nil {
			out = append(out, FixViolation{Path: name, Reason: err.Error()})
			continue
		}
		out = append(out, FixViolation{Path: filepath.ToSlash(rel), Reason: ViolateCreated, file: SnapshotFile{name: name, directory: directory}})
	}
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
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	return err == nil && file != nil && ast.IsGenerated(file)
}

func parses(src []byte) bool {
	_, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	return err == nil
}

// Undo replaces a leaf inside the already-open package directory. It never
// follows a substituted leaf or parent symlink when restoring source bytes.
func (v FixViolation) Undo(_ string) error {
	if v.file.directory == nil {
		return fmt.Errorf("restore %s: no source directory", v.Path)
	}
	if v.Pre == nil {
		if err := v.file.directory.root.Remove(v.file.name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", v.Path, err)
		}
		return nil
	}
	return replaceSource(v.file, v.Pre)
}

func replaceSource(file SnapshotFile, data []byte) error {
	name := ".gglint-fix-" + rand.Text()
	root := file.directory.root
	out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.mode.Perm())
	if err != nil {
		return fmt.Errorf("stage %s: %w", file.name, err)
	}
	_, err = out.Write(data)
	if err == nil {
		err = out.Chmod(file.mode.Perm())
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		if err = root.Rename(name, file.name); err != nil {
			return errors.Join(fmt.Errorf("publish %s: %w", file.name, err), dropTemp(root, name))
		}
		return nil
	}
	return errors.Join(fmt.Errorf("stage %s: %w", file.name, err), dropTemp(root, name))
}

// dropTemp removes a staged temporary after a failed write. A missing
// temporary is already gone; any other removal failure joins the error.
func dropTemp(root *os.Root, name string) error {
	if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove temporary %s: %w", name, err)
	}
	return nil
}

// ApplyFixed validates the complete staged batch before publishing any file.
// Policy refusals leave the batch unchanged; write errors restore prior writes.
func ApplyFixed(workDir string, pre GoSnapshot, fixed map[string][]byte, tests bool) ([]FixViolation, error) {
	violations := VerifyFixed(workDir, pre, map[string]bool{}, tests)
	if len(violations) > 0 {
		return violations, restoreViolations(workDir, violations)
	}
	batch := &stageBatch{applied: map[string]bool{}, contents: map[string][]byte{}}
	for filename, data := range fixed {
		staged, err := batch.add(workDir, pre, tests, filename, data)
		if err != nil {
			return nil, err
		}
		violations = append(violations, staged...)
	}
	slices.SortFunc(violations, func(a, b FixViolation) int { return strings.Compare(a.Path, b.Path) })
	if len(violations) > 0 {
		return violations, nil
	}
	return publishBatch(workDir, pre, batch, tests)
}

func restoreViolations(workDir string, violations []FixViolation) error {
	var failures []error
	for _, violation := range violations {
		if err := violation.Undo(workDir); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// snapshotOne records one selected source file. Non-regular files stay out:
// the verifier cannot own their bytes, and a staged fix that targets a
// skipped path is refused as outside the analysis scope.
func snapshotOne(out GoSnapshot, directories map[string]*snapshotDirectory, workDir, filename string) error {
	path := filepath.Clean(absolute(filename, workDir))
	rel, err := filepath.Rel(workDir, path)
	if err != nil {
		return err
	}
	directory, err := snapshotDir(directories, filepath.Dir(path))
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	info, err := directory.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := readRootFile(directory.root, name)
	if err != nil {
		return err
	}
	out[filepath.ToSlash(rel)] = SnapshotFile{Bytes: data, name: name, mode: info.Mode(), directory: directory}
	return nil
}

// snapshotDir returns the open handle for parent, opening it once per snapshot.
func snapshotDir(directories map[string]*snapshotDirectory, parent string) (*snapshotDirectory, error) {
	if directory := directories[parent]; directory != nil {
		return directory, nil
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, fmt.Errorf("open source directory %s: %w", parent, err)
	}
	names, err := rootNames(root)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	directory := &snapshotDirectory{root: root, path: parent, names: names}
	directories[parent] = directory
	return directory, nil
}

// stageBatch accumulates validated staged fixes before publication.
type stageBatch struct {
	applied  map[string]bool
	contents map[string][]byte
}

// add validates one staged fix into the batch, reporting its violations or a
// conflicting duplicate. Unchanged files add nothing.
func (b *stageBatch) add(workDir string, pre GoSnapshot, tests bool, filename string, data []byte) ([]FixViolation, error) {
	rel, err := filepath.Rel(workDir, absolute(filename, workDir))
	if err != nil {
		return nil, err
	}
	path := filepath.ToSlash(rel)
	before, exists := pre[path]
	if !exists {
		return []FixViolation{{Path: path, Reason: ViolateScope}}, nil
	}
	if bytes.Equal(before.Bytes, data) {
		return nil, nil
	}
	b.applied[path] = true
	if reason, refused := refuseChange(path, before.Bytes, data, b.applied, tests); refused {
		return []FixViolation{{Path: path, Reason: reason}}, nil
	}
	if !parses(before.Bytes) {
		return []FixViolation{{Path: path, Reason: ViolateParse}}, nil
	}
	if previous, exists := b.contents[path]; exists && !bytes.Equal(previous, data) {
		return nil, fmt.Errorf("conflicting staged fixes for %s", path)
	}
	b.contents[path] = data
	return nil, nil
}

// publishBatch writes validated contents and verifies the tree afterward.
// A write error restores prior writes; a post-write violation restores both
// the published batch and the violated files.
func publishBatch(workDir string, pre GoSnapshot, batch *stageBatch, tests bool) ([]FixViolation, error) {
	paths := make([]string, 0, len(batch.contents))
	for path := range batch.contents {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	var published []FixViolation
	for _, path := range paths {
		before := pre[path]
		if err := replaceSource(before, batch.contents[path]); err != nil {
			return nil, errors.Join(err, restoreViolations(workDir, published))
		}
		published = append(published, FixViolation{Path: path, Pre: before.Bytes, file: before})
	}
	violations := VerifyFixed(workDir, pre, batch.applied, tests)
	if len(violations) > 0 {
		return violations, errors.Join(restoreViolations(workDir, published), restoreViolations(workDir, violations))
	}
	return nil, nil
}
