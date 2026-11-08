package checkaligned

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestFixtures(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "aligned", "dotimport", "ignored")
}
