package checklocks

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestFixtures(t *testing.T) {
	for _, flag := range []string{"atomic", "wrappers", "inferred"} {
		if err := Analyzer.Flags.Set(flag, "true"); err != nil {
			t.Fatalf("set %s: %v", flag, err)
		}
	}
	analysistest.Run(t, analysistest.TestData(), Analyzer,
		"gvisor.dev/gvisor/tools/checklocks/test",
		"gvisor.dev/gvisor/tools/checklocks/test/crosspkg",
		"gvisor.dev/gvisor/tools/checklocks/test/atomic",
	)
}
