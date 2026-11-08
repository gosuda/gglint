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

	mu    sync.Mutex
	files map[*ast.File]*fileState
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
		files:    map[*ast.File]*fileState{},
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
			e.adopt(r, a, len(built))
		}
		out = append(out, built...)
	}
	return out
}

func (e *engine) adopt(r rules.Rule, a *analysis.Analyzer, siblings int) {
	check := ""
	if siblings > 1 {
		check = a.Name
	}
	a.Name = rules.AnalyzerName(r, a, siblings)
	run := a.Run
	a.Run = func(pass *analysis.Pass) (any, error) {
		filtered := *pass
		filtered.Report = func(d analysis.Diagnostic) {
			if e.keep(pass, r.Name, check, d) {
				pass.Report(d)
			}
		}
		return run(&filtered)
	}
}

func reportPanics(analyzers []*analysis.Analyzer) []*analysis.Analyzer {
	for _, a := range analyzers {
		run := a.Run
		a.Run = func(pass *analysis.Pass) (result any, err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("panic: %v\n%s", p, debug.Stack())
				}
			}()
			return run(pass)
		}
	}
	return analyzers
}

func (e *engine) keep(pass *analysis.Pass, rule, check string, d analysis.Diagnostic) bool {
	position := pass.Fset.Position(d.Pos)
	if !e.cfg.Tests && strings.HasSuffix(position.Filename, "_test.go") || e.excluded(position.Filename, rule, check, d.Message) {
		return false
	}
	file := fileAt(pass.Files, d.Pos)
	if file == nil {
		return true
	}
	state := e.stateOf(pass, file)
	return !state.generated && (rule == rules.NolintName || !e.suppressed(state, position.Line, rule, check))
}

func fileAt(files []*ast.File, pos token.Pos) *ast.File {
	for _, f := range files {
		if f.FileStart <= pos && pos < f.FileEnd {
			return f
		}
	}
	return nil
}

func (e *engine) stateOf(pass *analysis.Pass, file *ast.File) *fileState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state, ok := e.files[file]; ok {
		return state
	}
	state := &fileState{generated: ast.IsGenerated(file)}
	if src, err := pass.ReadFile(pass.Fset.File(file.FileStart).Name()); err == nil {
		state.directives = collectDirectives(pass.Fset, file, src)
	}
	e.files[file] = state
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
