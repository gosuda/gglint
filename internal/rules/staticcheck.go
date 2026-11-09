package rules

import (
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"

	"github.com/gosuda/gglint/internal/config"
)

func staticcheckRules() []Rule {
	return []Rule{
		{"staticcheck", config.Basic, originStaticcheck, "Staticcheck: SA bug checks; standard adds S and ST checks, strict adds QF quick fixes.", buildStaticcheck},
		{"unused", config.Basic, originStaticcheck, "Reports unused constants, variables, functions, types, and fields (staticcheck U1000).", single(unused.Analyzer.Analyzer)},
	}
}

func buildStaticcheck(cfg config.Config) []*analysis.Analyzer {
	prefixes, excluded := staticcheckPolicy(cfg.Mode)
	var selected []*analysis.Analyzer
	for _, a := range slices.Concat(staticcheck.Analyzers, simple.Analyzers, stylecheck.Analyzers, quickfix.Analyzers) {
		name := a.Analyzer.Name
		if slices.Contains(prefixes, strings.TrimRight(name, "0123456789")) && !slices.Contains(excluded, name) {
			selected = append(selected, a.Analyzer)
		}
	}
	return selected
}

func staticcheckPolicy(m config.Mode) (prefixes, excluded []string) {
	switch m {
	case config.Basic:
		return []string{"SA"}, nil
	case config.Standard:
		return []string{"SA", "S", "ST"}, strings.Fields("ST1000 ST1003 ST1016 ST1020 ST1021 ST1022")
	case config.Strict:
	}
	return []string{"SA", "S", "ST", "QF"}, strings.Fields("ST1000 ST1016 ST1020 ST1021 ST1022")
}
