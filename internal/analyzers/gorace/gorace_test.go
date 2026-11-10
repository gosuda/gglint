package gorace_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/gorace"
)

func TestAnalyzers(t *testing.T) {
	t.Parallel()
	patterns := map[string]string{
		"map":         "gorace/mapcheck",
		"fanout":      "gorace/fanout",
		"shared":      "gorace/shared",
		"doublecheck": "gorace/doublecheck",
		"subtest":     "gorace/subtest",
	}
	for _, a := range gorace.Analyzers() {
		analysistest.Run(t, analysistest.TestData(), a, patterns[a.Name])
	}
}
