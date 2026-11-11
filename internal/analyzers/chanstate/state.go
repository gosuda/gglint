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

func (v machineVisitor) step(n ast.Node, s chanState) chanState {
	if defersClose(v.info, n, v.key) {
		return s
	}
	if goCloses(v.info, n, v.key) {
		return stUnknown
	}
	if touchedNode(v.info, n, v.key, v.memo.touched) {
		return stUnknown
	}
	if isRangeAssignTarget(v.info, n, v.key, v.memo.targets) {
		return stUnknown
	}
	out := s
	for _, op := range nodeOps(v.info, n, v.key, v.memo.ops) {
		out = stepOp(op, out)
	}
	return out
}

func nodeOps(info *types.Info, n ast.Node, k flow.Key, memo map[ast.Node][]chanOp) []chanOp {
	ops, ok := memo[n]
	if !ok {
		ops = opsOf(info, n, k)
		memo[n] = ops
	}
	return ops
}

func touchedNode(info *types.Info, n ast.Node, k flow.Key, memo map[ast.Node]bool) bool {
	touched, ok := memo[n]
	if !ok {
		touched = literalTouch(info, n, k)
		memo[n] = touched
	}
	return touched
}

func defersClose(info *types.Info, n ast.Node, k flow.Key) bool {
	d, ok := n.(*ast.DeferStmt)
	if !ok {
		return false
	}
	ck, ok := closeKey(info, d.Call)
	return ok && ck == k
}

func goCloses(info *types.Info, n ast.Node, k flow.Key) bool {
	g, ok := n.(*ast.GoStmt)
	if !ok {
		return false
	}
	ck, ok := closeKey(info, g.Call)
	return ok && ck == k
}

func rangeAssignTargets(fn ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(fn, func(x ast.Node) bool {
		if r, ok := x.(*ast.RangeStmt); ok && (r.Tok == token.ASSIGN || r.Tok == token.DEFINE) {
			if r.Key != nil {
				out[r.Key] = true
			}
			if r.Value != nil {
				out[r.Value] = true
			}
		}
		return true
	})
	return out
}

func isRangeAssignTarget(info *types.Info, n ast.Node, k flow.Key, targets map[ast.Node]bool) bool {
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
	return targets[n]
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
