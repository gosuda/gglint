package checkimports

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/config"
)

func TestFixtures(t *testing.T) {
	deny := []config.Deny{
		{Path: "archive/tar"},
		{Path: "crypto"},
		{Path: "net/...", AllowFrom: []string{"allowed/..."}},
		{Path: "encoding/json", AllowFrom: []string{"jsonowner"}},
	}
	analysistest.Run(t, analysistest.TestData(), New(deny),
		"unsafeuse", "consumer", "network", "zipuser",
		"allowed", "allowed/sub", "allowedx", "jsonowner", "jsonowner/sub",
	)
}
