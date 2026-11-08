package stackaddr_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/stackaddr"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), stackaddr.Analyzer, "stackaddr")
}
