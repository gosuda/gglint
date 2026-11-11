package chanstate

import "testing"

func TestStepJoin(t *testing.T) {
	t.Parallel()
	if joinState(stOpen, stOpen) != stOpen {
		t.Error("same states stay")
	}
	if joinState(stOpen, stClosed) != stUnknown {
		t.Error("different states merge to unknown")
	}
	if stepOp(chanOp{Kind: opClose}, stOpen) != stClosed {
		t.Error("close closes")
	}
	if stepOp(chanOp{Kind: opAssign}, stOpen) != stUnknown {
		t.Error("assign blurs")
	}
	if stepOp(chanOp{Kind: opSend}, stOpen) != stOpen {
		t.Error("send keeps")
	}
	if defState(chanOp{Kind: opDefine, Make: true}) != stOpen {
		t.Error("make opens")
	}
	if defState(chanOp{Kind: opDefine, NilInit: true}) != stNil {
		t.Error("bare var is nil")
	}
	if defState(chanOp{Kind: opDefine}) != stUnknown {
		t.Error("other defines blur")
	}
}
