package vettool

import (
	"fmt"
	"go/ast"
	"go/token"
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

	mu      sync.Mutex
	files   map[*ast.File]func() *fileState
	sources map[string]func() ([]byte, error)
}

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
			if e.keep(pass, r.Name, check, d) {
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

func (e *engine) keep(pass *analysis.Pass, rule, check string, d analysis.Diagnostic) bool {
	filename := realName(pass.Fset, d.Pos)
	if !e.cfg.Tests && strings.HasSuffix(filename, "_test.go") || e.excluded(filename, rule, check, d.Message) {
		return false
	}
	file := fileAt(pass.Files, d.Pos)
	if file == nil {
		return true
	}
	state := e.stateOf(pass, file)
	return !state.generated && (rule == rules.NolintName || !e.suppressed(state, pass.Fset.Position(d.Pos).Line, rule, check))
}

func realName(fset *token.FileSet, pos token.Pos) string {
	if f := fset.File(pos); f != nil {
		return f.Name()
	}
	return fset.Position(pos).Filename
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
