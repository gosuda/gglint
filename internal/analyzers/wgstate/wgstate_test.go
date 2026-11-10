package wgstate_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/wgstate"
)

func TestAnalyzers(t *testing.T) {
	t.Parallel()
	for _, a := range wgstate.Analyzers() {
		analysistest.Run(t, analysistest.TestData(), a, a.Name)
	}
}
