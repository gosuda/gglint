package goconst_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/goconst"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), goconst.Analyzer, "consts")
}
