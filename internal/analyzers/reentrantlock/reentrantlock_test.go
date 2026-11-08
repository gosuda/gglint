package reentrantlock_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/reentrantlock"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), reentrantlock.Analyzer, "reentrantlock", "heldpath")
}
