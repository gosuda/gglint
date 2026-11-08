package unsizedint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/unsizedint"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unsizedint.Analyzer, "unsizedint")
}
