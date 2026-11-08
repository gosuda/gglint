package analyzertest

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

type factTolerant struct{ t testing.TB }

func (f factTolerant) Errorf(format string, args ...any) {
	f.t.Helper()
	message := fmt.Sprintf(format, args...)
	if strings.Contains(message, ": unexpected fact: ") {
		return
	}
	f.t.Error(message)
}

func RunIgnoringFacts(tb testing.TB, dir string, a *analysis.Analyzer, patterns ...string) []*analysistest.Result {
	tb.Helper()
	return analysistest.Run(factTolerant{tb}, dir, a, patterns...)
}
