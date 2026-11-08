package goroutine_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/goroutine"
	"github.com/gosuda/gglint/internal/config"
)

func TestStandard(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), goroutine.New(config.Standard), "standard")
}

func TestStrict(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), goroutine.New(config.Strict), "strict")
}
