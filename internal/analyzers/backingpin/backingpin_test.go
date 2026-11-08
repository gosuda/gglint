package backingpin_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/backingpin"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), backingpin.Analyzer, "backingpin")
}
