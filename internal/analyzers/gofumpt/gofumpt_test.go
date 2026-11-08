package gofumpt_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/gofumpt"
)

func TestAnalyzer(t *testing.T) {
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), gofumpt.Analyzer, "fumpt")
}
