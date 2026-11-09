package gosec_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/gosec"
)

func TestReportsIssues(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), gosec.New(nil), "sec")
}

func TestExcludedRulesStaySilent(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), gosec.New([]string{"G401", "G501"}), "secexcluded")
}
