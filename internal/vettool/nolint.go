package vettool

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

func (e *engine) nolintAnalyzer(requires []*analysis.Analyzer) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     rules.NolintName,
		Doc:      "check //nolint directives for unknown rules, missing names or reasons, and unused suppressions",
		Requires: requires,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, d := range e.stateOf(pass, file).directives {
					for _, message := range e.problems(d) {
						e.report(pass, d, message)
					}
				}
			}
			return nil, nil
		},
	}
}

func (e *engine) report(pass *analysis.Pass, d *directive, message string) {
	diagnostic := analysis.Diagnostic{Pos: d.slash, End: d.end, Message: message}
	if e.keep(pass, rules.NolintName, "", diagnostic) {
		pass.Report(diagnostic)
	}
}

func (e *engine) problems(d *directive) []string {
	if d.malformed {
		return []string{"malformed nolint directive"}
	}
	var out []string
	if d.bare() && e.cfg.Mode >= config.Standard {
		out = append(out, "nolint directive must name rules")
	}
	if d.reason == "" && e.cfg.Mode >= config.Strict {
		out = append(out, "nolint directive needs a reason after //")
	}
	out = append(out, e.tokenProblems(d)...)
	return append(out, e.unused(d)...)
}

func (e *engine) tokenProblems(d *directive) []string {
	var out []string
	for _, t := range d.tokens {
		rule, _, _ := strings.Cut(t, ":")
		switch {
		case rule == rules.TypecheckName:
			out = append(out, "typecheck findings cannot be suppressed")
		case !e.known[rule]:
			out = append(out, fmt.Sprintf("unknown nolint rule %q", rule))
		}
	}
	return out
}

func (e *engine) unused(d *directive) []string {
	if !e.allSelected(d) {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, t := range d.tokens {
		if !d.matched[t] {
			out = append(out, "unused nolint directive for "+t)
		}
	}
	return out
}

func (e *engine) allSelected(d *directive) bool {
	for _, t := range d.tokens {
		rule, _, _ := strings.Cut(t, ":")
		if !e.selected[rule] || rule == rules.TypecheckName {
			return false
		}
	}
	return true
}
