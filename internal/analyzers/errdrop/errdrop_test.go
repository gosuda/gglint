package errdrop_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/errdrop"
	"github.com/gosuda/gglint/internal/analyzertest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analyzertest.RunIgnoringFacts(t, analysistest.TestData(), errdrop.Analyzer, "errdrop")
}
