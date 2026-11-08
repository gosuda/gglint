package rules

import (
	"fmt"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

const flagTrue = "true"

func thirdPartyRules() []Rule {
	return slices.Concat(thirdPartyBasicRules(), thirdPartyStandardRules(), thirdPartyStrictRules())
}

func must[T any](rule string, v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("rules: %s: %v", rule, err))
	}
	return v
}

func setFlags(rule string, a *analysis.Analyzer, flags map[string]string) *analysis.Analyzer {
	for name, value := range flags {
		a = must(rule, a, a.Flags.Set(name, value))
	}
	return a
}

func construct(newAnalyzer func() *analysis.Analyzer) func(config.Config) []*analysis.Analyzer {
	return func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{newAnalyzer()} }
}
