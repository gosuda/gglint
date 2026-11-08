package complexity_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/complexity"
	"github.com/gosuda/gglint/internal/config"
)

func TestComplexity(t *testing.T) {
	cases := []struct {
		pkg    string
		limits config.Complexity
	}{
		{"nesting", config.Complexity{MaxNesting: 3}},
		{"cognitive", config.Complexity{MaxCognitive: 5}},
		{"cyclomatic", config.Complexity{MaxCyclomatic: 4}},
		{"lines", config.Complexity{MaxLines: 10}},
		{"params", config.Complexity{MaxParams: 3, MaxBoolParams: 1}},
		{"interfaces", config.Complexity{MaxInterfaceMethods: 3}},
		{"filelines", config.Complexity{MaxFileLines: 20}},
		{"testskip", config.Complexity{MaxNesting: 2, MaxLines: 12, MaxFileLines: 120}},
		{"order", config.Complexity{
			MaxCognitive: 2, MaxCyclomatic: 2, MaxNesting: 1, MaxLines: 6, MaxParams: 1, MaxBoolParams: 1,
		}},
		{"off", config.Complexity{}},
	}
	for _, c := range cases {
		t.Run(c.pkg, func(t *testing.T) {
			analysistest.Run(t, analysistest.TestData(), complexity.New(c.limits), c.pkg)
		})
	}
}
