package chanstate_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/chanstate"
)

func TestAnalyzers(t *testing.T) {
	t.Parallel()
	for _, a := range chanstate.Analyzers() {
		t.Run(a.Name, func(t *testing.T) {
			t.Parallel()
			analysistest.Run(t, analysistest.TestData(), a, a.Name)
		})
	}
}
