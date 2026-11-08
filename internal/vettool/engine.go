package vettool

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
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
	files map[string]*fileState
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
		files:    map[string]*fileState{},
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

func (e *engine) withNolint(built []*analysis.Analyzer) []*analysis.Analyzer {
	if !e.selected[rules.NolintName] {
		return built
	}
	return append(built, e.nolintAnalyzer(built))
}

func (e *engine) adopt(r rules.Rule, a *analysis.Analyzer, siblings int) {
	check := ""
	if siblings > 1 {
		check = a.Name
	}
	a.Name = rules.AnalyzerName(r, a, siblings)
	run := a.Run
	a.Run = func(pass *analysis.Pass) (result any, err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		filtered := *pass
		filtered.Report = func(d analysis.Diagnostic) {
			if e.keep(pass, r.Name, check, d) {
				pass.Report(d)
			}
		}
		return run(&filtered)
	}
}

func (e *engine) keep(pass *analysis.Pass, rule, check string, d analysis.Diagnostic) bool {
	file := fileAt(pass.Files, d.Pos)
	if file == nil {
		return true
	}
	position := pass.Fset.Position(d.Pos)
	state := e.stateOf(pass, file, position.Filename)
	switch {
	case state.generated:
		return false
	case !e.cfg.Tests && strings.HasSuffix(position.Filename, "_test.go"):
		return false
	case e.excluded(position.Filename, rule, check, d.Message):
		return false
	}
	return rule == rules.NolintName || !e.suppressed(state, position.Line, rule, check)
}

func fileAt(files []*ast.File, pos token.Pos) *ast.File {
	for _, f := range files {
		if f.FileStart <= pos && pos < f.FileEnd {
			return f
		}
	}
	return nil
}

func (e *engine) stateOf(pass *analysis.Pass, file *ast.File, filename string) *fileState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state, ok := e.files[filename]; ok {
		return state
	}
	src := readSource(pass, filename)
	state := &fileState{
		generated:  ast.IsGenerated(file),
		directives: collectDirectives(pass.Fset, file, src),
	}
	e.files[filename] = state
	return state
}

func readSource(pass *analysis.Pass, filename string) []byte {
	if pass.ReadFile != nil {
		if src, err := pass.ReadFile(filename); err == nil {
			return src
		}
	}
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil
	}
	return src
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
		if d.malformed || !d.covers(line) {
			continue
		}
		hit = d.match(rule, check) || hit
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
