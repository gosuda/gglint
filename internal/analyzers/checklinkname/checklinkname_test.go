package checklinkname

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckLinkname(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), Analyzer, "linkname/remote", "linkname/local", "linkname/cmd")
}
