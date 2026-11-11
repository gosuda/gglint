package rules

import (
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/unused"

	"github.com/gosuda/gglint/internal/config"
)

func staticcheckRules() []Rule {
	return []Rule{
		{"staticcheck", config.Basic, originStaticcheck, "Staticcheck SA bug checks; standard adds SA1019 (deprecated API use).", ReportEverywhere, buildStaticcheck},
		{"gosimple", config.Standard, originStaticcheck, "Staticcheck S simplifications that carry a fix.", ReportWithFix, buildGosimple},
		{"unused", config.Basic, originStaticcheck, "Reports unused constants, variables, functions, types, and fields (staticcheck U1000).", ReportEverywhere, single(unused.Analyzer.Analyzer)},
	}
}

func buildStaticcheck(cfg config.Config) []*analysis.Analyzer {
	excluded := staticcheckExcluded(cfg.Mode)
	var selected []*analysis.Analyzer
	for _, a := range staticcheck.Analyzers {
		name := a.Analyzer.Name
		if strings.TrimRight(name, "0123456789") == "SA" && !slices.Contains(excluded, name) {
			selected = append(selected, a.Analyzer)
		}
	}
	return selected
}

func buildGosimple(config.Config) []*analysis.Analyzer {
	var selected []*analysis.Analyzer
	for _, a := range simple.Analyzers {
		selected = append(selected, a.Analyzer)
	}
	return selected
}

func staticcheckExcluded(m config.Mode) []string {
	if m == config.Basic {
		return strings.Fields("SA1019 SA4006")
	}
	return strings.Fields("SA4006")
}
