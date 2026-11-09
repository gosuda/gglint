package checkconst

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckconst(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), Analyzer, "checkconst/layout", "checkconst/consts", "checkconst/asm")
}
