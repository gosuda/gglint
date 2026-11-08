package misspell_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/misspell"
)

func TestAnalyzer(t *testing.T) {
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), misspell.Analyzer, "spelling")
}
