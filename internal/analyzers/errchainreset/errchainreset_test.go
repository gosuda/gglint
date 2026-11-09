package errchainreset_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/errchainreset"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), errchainreset.Analyzer, "errchainreset")
}
