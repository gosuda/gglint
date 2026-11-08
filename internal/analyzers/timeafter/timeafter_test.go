package timeafter_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/timeafter"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), timeafter.Analyzer, "timeafter")
}
