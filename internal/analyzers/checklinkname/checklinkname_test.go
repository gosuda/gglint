package checklinkname

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckLinkname(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "linkname/remote", "linkname/local", "linkname/cmd")
}
