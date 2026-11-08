package structtagmalformed_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/structtagmalformed"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), structtagmalformed.Analyzer, "structtagmalformed")
}
