package closer_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/closer"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), closer.Analyzer, "closer")
}
