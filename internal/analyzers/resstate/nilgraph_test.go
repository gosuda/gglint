package resstate

import (
	"testing"

	"github.com/gosuda/gglint/internal/flow"
)

func TestOuterAssignLiveNilGraphStaysSilent(t *testing.T) {
	t.Parallel()
	if outerAssignLive(nil, loopCtx{}, nil, nil, flow.Key{}, "Bytes") {
		t.Fatal("nil graph must stay silent")
	}
}
