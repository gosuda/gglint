package unparam_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/unparam"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unparam.Analyzer, "params")
}
