package capturerace_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/capturerace"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), capturerace.Analyzer, "capturerace")
}
