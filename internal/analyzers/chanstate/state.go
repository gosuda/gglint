package chanstate

import (
	"go/ast"
	"go/token"
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

func stepNode(info *types.Info, fn, n ast.Node, k flow.Key, s chanState) chanState {
	if d, ok := n.(*ast.DeferStmt); ok {
		if ck, ok := closeKey(info, d.Call); ok && ck == k {
			return s
		}
	}
	if literalTouch(info, n, k) {
		return stUnknown
	}
	if isRangeAssignTarget(info, fn, n, k) {
		return stUnknown
	}
	out := s
	for _, op := range opsOf(info, n, k) {
		out = stepOp(op, out)
	}
	return out
}

func isRangeAssignTarget(info *types.Info, fn, n ast.Node, k flow.Key) bool {
	e, ok := n.(ast.Expr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	if kk, ok := keyOfIdent(info, id); !ok || kk != k {
		return false
	}
	return rangeAssignsTarget(fn, n)
}

func rangeAssignsTarget(fn, n ast.Node) bool {
	found := false
	ast.Inspect(fn, func(x ast.Node) bool {
		if found {
			return false
		}
		found = rangeStmtTargets(x, n)
		return !found
	})
	return found
}

func rangeStmtTargets(x, n ast.Node) bool {
	r, ok := x.(*ast.RangeStmt)
	if !ok {
		return false
	}
	if r.Tok != token.ASSIGN && r.Tok != token.DEFINE {
		return false
	}
	return r.Key == n || r.Value == n
}

func stepOp(op chanOp, s chanState) chanState {
	switch op.Kind {
	case opClose:
		return stClosed
	case opDefine:
		return defState(op)
	case opAssign:
		return stUnknown
	case opSend, opRecv:
		return s
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
