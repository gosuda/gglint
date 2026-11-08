package noinit_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/noinit"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), noinit.Analyzer, "noinit")
}
