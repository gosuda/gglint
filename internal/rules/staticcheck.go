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
		{
			Name:   "staticcheck",
			Mode:   config.Basic,
			Origin: originStaticcheck,
			Doc:    "Staticcheck: SA bug checks; standard adds S and ST checks, strict adds QF quick fixes.",
			Build:  buildStaticcheck,
		},
		{
			Name:   "unused",
			Mode:   config.Basic,
			Origin: originStaticcheck,
			Doc:    "Reports unused constants, variables, functions, types, and fields (staticcheck U1000).",
			Build:  single(unused.Analyzer.Analyzer),
		},
	}
}

func buildStaticcheck(cfg config.Config) []*analysis.Analyzer {
	prefixes, excluded := staticcheckPolicy(cfg.Mode)
	all := slices.Concat(staticcheck.Analyzers, simple.Analyzers, stylecheck.Analyzers, quickfix.Analyzers)
	selected := make([]*analysis.Analyzer, 0, len(all))
	for _, a := range all {
		if staticcheckSelects(a.Analyzer.Name, prefixes, excluded) {
			selected = append(selected, a.Analyzer)
		}
	}
	return selected
}

func staticcheckSelects(name string, prefixes, excluded []string) bool {
	prefix := strings.TrimRight(name, "0123456789")
	return slices.Contains(prefixes, prefix) && !slices.Contains(excluded, name)
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
