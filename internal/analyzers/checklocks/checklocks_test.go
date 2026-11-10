package checklocks

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzertest"
)

func TestFixtures(t *testing.T) {
	for _, flag := range []string{"atomic", "wrappers", "inferred"} {
		previous := Analyzer.Flags.Lookup(flag).Value.String()
		if err := Analyzer.Flags.Set(flag, "true"); err != nil {
			t.Fatalf("set %s: %v", flag, err)
		}
		t.Cleanup(func() {
			if err := Analyzer.Flags.Set(flag, previous); err != nil {
				t.Errorf("restore %s: %v", flag, err)
			}
		})
	}
	analyzertest.RunIgnoringFacts(t, analysistest.TestData(), Analyzer,
		"gvisor.dev/gvisor/tools/checklocks/test",
		"gvisor.dev/gvisor/tools/checklocks/test/crosspkg",
		"gvisor.dev/gvisor/tools/checklocks/test/atomic",
	)
}
