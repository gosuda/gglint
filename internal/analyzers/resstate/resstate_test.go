package resstate_test

import (
	"runtime"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/resstate"
)

func TestAnalyzers(t *testing.T) {
	t.Parallel()
	for _, a := range resstate.Analyzers() {
		t.Run(a.Name, func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" && a.Name == "useafterfree" {
				t.Skip("fixture relies on syscall.Mmap/Munmap, unavailable on Windows")
			}
			analysistest.Run(t, analysistest.TestData(), a, a.Name)
		})
	}
}
