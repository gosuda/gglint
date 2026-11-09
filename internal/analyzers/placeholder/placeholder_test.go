package placeholder_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/placeholder"
	"github.com/gosuda/gglint/internal/config"
)

func TestStandard(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), placeholder.New(config.Standard), "standard")
}

func TestStrict(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), placeholder.New(config.Strict), "strict")
}
