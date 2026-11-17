package chanblock_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/chanblock"
)

func TestDefaultSelectBounds(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"communications", "operands", "bodies", "accounting", "cancellation"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			analysistest.Run(t, analysistest.TestData(), chanblock.Analyzer, "defaultselect/"+name)
		})
	}
}
