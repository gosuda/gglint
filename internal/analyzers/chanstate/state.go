package chanstate

import (
	"go/ast"
	"go/types"

	"github.com/gosuda/gglint/internal/flow"
)

type chanState int

const (
	stUnknown chanState = iota
	stNil
	stOpen
	stClosed
)

func joinState(a, b chanState) chanState {
	if a == b {
		return a
	}
	return stUnknown
}

func stepNode(info *types.Info, n ast.Node, k flow.Key, s chanState) chanState {
	if d, ok := n.(*ast.DeferStmt); ok {
		if ck, ok := closeKey(info, d.Call); ok && ck == k {
			return s
		}
	}
	if literalTouch(info, n, k) {
		return stUnknown
	}
	out := s
	for _, op := range opsOf(info, n, k) {
		out = stepOp(op, out)
	}
	return out
}

func stepOp(op chanOp, s chanState) chanState {
	switch op.Kind {
	case opClose:
		return stClosed
	case opDefine:
		return defState(op)
	case opAssign:
		return stUnknown
	}
	return s
}

func defState(op chanOp) chanState {
	switch {
	case op.Make:
		return stOpen
	case op.NilInit:
		return stNil
	}
	return stUnknown
}
