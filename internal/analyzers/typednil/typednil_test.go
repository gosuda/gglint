package typednil_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/typednil"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), typednil.Analyzer, "typednil")
}
