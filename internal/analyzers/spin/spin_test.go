package spin_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/spin"
)

func TestPoll(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), spin.Analyzers()[0], "poll")
}

func TestClosedrecv(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), spin.Analyzers()[1], "closedrecv")
}

func TestBusywait(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), spin.Analyzers()[2], "busywait")
}

func TestStalecas(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), spin.Analyzers()[3], "stalecas")
}

func TestSkipadvance(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), spin.Analyzers()[4], "skipadvance")
}
