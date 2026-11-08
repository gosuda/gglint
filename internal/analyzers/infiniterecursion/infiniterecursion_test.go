package infiniterecursion_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/infiniterecursion"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), infiniterecursion.Analyzer, "infiniterecursion")
}
