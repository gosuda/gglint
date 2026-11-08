package chanblock_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/chanblock"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), chanblock.Analyzer, "chanblock")
}
