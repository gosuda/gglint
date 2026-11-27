package vettool

import (
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

type engine struct {
	cfg      config.Config
	excludes []config.CompiledExclude
	known    map[string]bool
	selected map[string]bool
	// fix reports diagnostics whose suggested fix is refused instead of
	// applying it. It is set for `go vet -fix` runs and stays false for
	// report runs, so plain `gglint run` never prints refusal lines.
	fix bool
	// err receives refusal lines; it is os.Stderr in production and a
	// test-owned buffer in tests, so parallel tests never swap the global
	// os.Stderr.
	err     io.Writer
	refused map[string]bool

	mu      sync.Mutex
	files   map[*ast.File]func() *fileState
	sources map[string]func() ([]byte, error)
}

// Reasons a suggested fix is refused. The strings are the publication
// contract of `gglint run -fix`: they appear verbatim on stderr and in the
// README section "Automatic fixes".
const (
	refuseScope = "outside the analysis scope"
	refuseGen   = "generated file"
	refuseTests = "test file excluded when tests are off"
)

type fileState struct {
	generated  bool
	directives []*directive
}

func newEngine(cfg config.Config) (*engine, []rules.Rule, error) {
	selected, err := rules.Select(cfg)
	if err != nil {
		return nil, nil, err
	}
	excludes, err := config.CompileExcludes(cfg.Excludes)
	if err != nil {
		return nil, nil, err
	}
	e := &engine{
		cfg:      cfg,
		excludes: excludes,
		known:    names(rules.All()),
		selected: names(selected),
		files:    map[*ast.File]func() *fileState{},
		sources:  map[string]func() ([]byte, error){},
	}
	return e, selected, nil
}

func names(rs []rules.Rule) map[string]bool {
	out := make(map[string]bool, len(rs))
	for _, r := range rs {
		out[r.Name] = true
	}
	return out
}

func (e *engine) analyzers(selected []rules.Rule) []*analysis.Analyzer {
	var out []*analysis.Analyzer
	for _, r := range selected {
		if r.Build == nil {
			continue
		}
		built := r.Build(e.cfg)
		for _, a := range built {
			out = append(out, e.adopt(r, a, len(built)))
		}
	}
	return out
}

func (e *engine) adopt(r rules.Rule, a *analysis.Analyzer, siblings int) *analysis.Analyzer {
	check := ""
	if siblings > 1 {
		check = a.Name
	}
	wrapped := *a
	wrapped.Name = rules.AnalyzerName(r, a, siblings)
	run := a.Run
	wrapped.Run = func(pass *analysis.Pass) (any, error) {
		filtered := *pass
		filtered.ReadFile = func(name string) ([]byte, error) { return e.source(pass, name) }
		filtered.Report = func(d analysis.Diagnostic) {
			if e.keep(pass, r, check, d) {
				pass.Report(d)
			}
		}
		return run(&filtered)
	}
	return &wrapped
}

func reportPanics(analyzers []*analysis.Analyzer) []*analysis.Analyzer {
	out := make([]*analysis.Analyzer, len(analyzers))
	for i, a := range analyzers {
		wrapped := *a
		run := a.Run
		wrapped.Run = func(pass *analysis.Pass) (result any, err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
				}
			}()
			return run(pass)
		}
		out[i] = &wrapped
	}
	return out
}

func (e *engine) keep(pass *analysis.Pass, r rules.Rule, check string, d analysis.Diagnostic) bool {
	keep, reason := e.admit(pass, r, check, d)
	// A dropped diagnostic that carries edits would have rewritten the file
	// under `go vet -fix`. Refusing it is a safety decision, so a fix run
	// says so on stderr; a report run stays silent.
	if !keep && reason != "" && hasEdits(d) {
		e.refuse(pass, d, reason)
	}
	if keep && e.fix {
		for _, fix := range d.SuggestedFixes {
			for _, edit := range fix.TextEdits {
				if !e.admitEdit(pass, r, check, d, edit) {
					return false
				}
			}
		}
	}
	return keep
}

// admit decides whether a diagnostic stays in the report. The refusal reason
// is empty when nothing is refused: the diagnostic carries no fix, or the
// user asked to skip it through configuration or a directive.
func (e *engine) admit(pass *analysis.Pass, r rules.Rule, check string, d analysis.Diagnostic) (keep bool, reason string) {
	if pass.Fset.File(d.Pos) == nil {
		return false, ""
	}
	filename := realName(pass.Fset, d.Pos)
	if drop, reason := e.filteredOut(r, filename, d); drop {
		return false, reason
	}
	if e.excluded(filename, r.Name, check, d.Message) {
		return false, ""
	}
	// A diagnostic outside both the analysis root and the package's own
	// directory — a cgo build-cache artifact, a //line remap, a stdlib
	// position — reports a file the user cannot act on.
	if e.outOfScope(pass, filename) {
		return false, refuseScope
	}
	return e.fileAdmit(pass, r, check, d)
}

// filteredOut reports a test-scope or fixless drop. Only the tests-off drop
// refuses a fix; the rest follow user configuration.
func (e *engine) filteredOut(r rules.Rule, filename string, d analysis.Diagnostic) (drop bool, reason string) {
	if r.Reports == rules.ReportWithFix && !hasEdits(d) {
		return true, ""
	}
	if !strings.HasSuffix(filename, "_test.go") {
		return false, ""
	}
	if !e.cfg.Tests {
		return true, refuseTests
	}
	return r.Reports == rules.ReportOutsideTests, ""
}

func (e *engine) outOfScope(pass *analysis.Pass, filename string) bool {
	return !insideDir(e.cfg.ExcludeBase, filename) && !insideDir(pkgDir(pass), filename)
}

func (e *engine) fileAdmit(pass *analysis.Pass, r rules.Rule, check string, d analysis.Diagnostic) (keep bool, reason string) {
	file := fileAt(pass.Files, d.Pos)
	if file == nil {
		return true, ""
	}
	state := e.stateOf(pass, file)
	if state.generated {
		return false, refuseGen
	}
	line := pass.Fset.PositionFor(d.Pos, false).Line
	if r.Name == rules.NolintName || !e.suppressed(state, line, r.Name, check) {
		return true, ""
	}
	return false, ""
}

// refuse records a refused fix for a `go vet -fix` run. Report runs ignore
// refusals so their output never changes.
func (e *engine) refuse(pass *analysis.Pass, d analysis.Diagnostic, reason string) {
	if !e.fix {
		return
	}
	filename := realName(pass.Fset, d.Pos)
	rel := filename
	if r, err := filepath.Rel(e.cfg.ExcludeBase, filename); err == nil && !climbsOut(r) {
		rel = filepath.ToSlash(r)
	}
	key := rel + "\x00" + reason
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.refused == nil {
		e.refused = map[string]bool{}
	}
	if e.refused[key] {
		return
	}
	e.refused[key] = true
	out := e.err
	if out == nil {
		out = os.Stderr
	}
	fmt.Fprintf(out, "gglint: refused %s: %s\n", rel, reason)
}

func hasEdits(d analysis.Diagnostic) bool {
	for _, fix := range d.SuggestedFixes {
		if len(fix.TextEdits) > 0 {
			return true
		}
	}
	return false
}

func realName(fset *token.FileSet, pos token.Pos) string {
	if f := fset.File(pos); f != nil {
		return f.Name()
	}
	return fset.Position(pos).Filename
}

// insideDir reports whether name resolves inside dir. A relative name is
// treated as a path inside dir unless it climbs out with "..".
func insideDir(dir, name string) bool {
	if dir == "" || name == "" {
		return false
	}
	if !filepath.IsAbs(name) && !rooted(name) {
		return !climbsOut(name)
	}
	rel, err := filepath.Rel(onVolume(dir), onVolume(name))
	return err == nil && !climbsOut(rel)
}

// climbsOut reports whether the relative path rel leaves its base directory.
func climbsOut(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// onVolume resolves a volume-relative path like \tmp\x on Windows on the
// current volume, because it is not an ordinary relative name. Every other
// path comes back unchanged.
func onVolume(path string) string {
	if filepath.IsAbs(path) || !rooted(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// rooted reports whether name begins at a filesystem root without a volume,
// like \tmp\x on Windows. Unix paths are already absolute at that point.
func rooted(name string) bool {
	return name[0] == '/' || name[0] == '\\'
}

// pkgDir returns the directory that holds the package's sources: the mode of
// pass.Files' directories, excluding cgo build-cache artifacts so they cannot
// win the vote even when they outnumber ordinary sources.
func pkgDir(pass *analysis.Pass) string {
	counts := map[string]int{}
	for _, f := range pass.Files {
		tf := pass.Fset.File(f.FileStart)
		if tf == nil || isCgoArtifact(tf.Name()) {
			continue
		}
		counts[filepath.Dir(tf.Name())]++
	}
	best, n := "", -1
	for d, c := range counts {
		if c > n || (c == n && d < best) {
			best, n = d, c
		}
	}
	return best
}

// isCgoArtifact reports whether name looks like a file cmd/cgo synthesizes
// into the build cache: _cgo_gotypes.go, _cgo_import.go, _cgo_export.go, and
// the *.cgo1.go copies of cgo sources.
func isCgoArtifact(name string) bool {
	base := filepath.Base(name)
	return strings.HasPrefix(base, "_cgo_") || strings.HasSuffix(base, ".cgo1.go")
}

func fileAt(files []*ast.File, pos token.Pos) *ast.File {
	for _, f := range files {
		if f.FileStart <= pos && pos < f.FileEnd {
			return f
		}
	}
	return nil
}

func (e *engine) source(pass *analysis.Pass, name string) ([]byte, error) {
	return e.sourceOnce(pass, name)()
}

func (e *engine) sourceOnce(pass *analysis.Pass, name string) func() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	read, ok := e.sources[name]
	if !ok {
		read = sync.OnceValues(func() ([]byte, error) { return pass.ReadFile(name) })
		e.sources[name] = read
	}
	return read
}

func (e *engine) stateOf(pass *analysis.Pass, file *ast.File) *fileState {
	return e.stateOnce(pass, file)()
}

func (e *engine) stateOnce(pass *analysis.Pass, file *ast.File) func() *fileState {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.files[file]
	if !ok {
		state = sync.OnceValue(func() *fileState { return e.scan(pass, file) })
		e.files[file] = state
	}
	return state
}

func (e *engine) scan(pass *analysis.Pass, file *ast.File) *fileState {
	state := &fileState{generated: ast.IsGenerated(file)}
	if src, err := e.source(pass, pass.Fset.File(file.FileStart).Name()); err == nil {
		state.directives = collectDirectives(pass.Fset, file, src)
	}
	return state
}

func (e *engine) excluded(filename, rule, check, message string) bool {
	rel := filepath.ToSlash(filename)
	if r, err := filepath.Rel(e.cfg.ExcludeBase, filename); err == nil {
		rel = filepath.ToSlash(r)
	}
	for _, x := range e.excludes {
		if x.Matches(rel, rule, check, message) {
			return true
		}
	}
	return false
}

func (e *engine) suppressed(state *fileState, line int, rule, check string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	hit := false
	for _, d := range state.directives {
		if d.covers(line) {
			hit = d.match(rule, check) || hit
		}
	}
	return hit
}

func (d *directive) match(rule, check string) bool {
	if d.bare() {
		return true
	}
	hit := false
	for _, t := range d.tokens {
		if config.MatchToken(t, rule, check) {
			d.matched[t] = true
			hit = true
		}
	}
	return hit
}

// admitEdit reports whether one suggested edit stays in a fix run, refusing
// it on stderr when it does not.
func (e *engine) admitEdit(pass *analysis.Pass, r rules.Rule, check string, d analysis.Diagnostic, edit analysis.TextEdit) bool {
	part := d
	part.Pos, part.End = edit.Pos, edit.End
	admitted, why := e.admit(pass, r, check, part)
	if target, ok := editTarget(pass, edit.Pos); ok {
		if !strings.HasSuffix(realName(pass.Fset, edit.Pos), ".go") {
			admitted, why = false, refuseScope
		} else if e.stateOf(pass, target).generated {
			admitted, why = false, refuseGen
		}
	} else {
		admitted, why = false, refuseScope
	}
	if !admitted && why != "" {
		e.refuse(pass, part, why)
	}
	return admitted
}

// editTarget returns the file holding pos, or false when no file does.
func editTarget(pass *analysis.Pass, pos token.Pos) (*ast.File, bool) {
	positionFile := pass.Fset.File(pos)
	if positionFile == nil {
		return nil, false
	}
	for _, file := range pass.Files {
		if pass.Fset.File(file.Pos()) == positionFile {
			return file, true
		}
	}
	return nil, false
}
