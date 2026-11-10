package chanstate

import (
	"go/ast"
	"go/types"

	"github.com/gosuda/gglint/internal/flow"
)

func drainSwitchRecvs(info *types.Info, s *ast.SwitchStmt, k flow.Key) (int64, bool) {
	header := stmtRecvs(info, s.Init, k) + uncondRecvs(info, s.Tag, k)
	if s.Body == nil {
		return header, true
	}
	if v, ok := constTag(info, s.Tag); ok {
		return drainSwitchConst(info, s, k, header, v)
	}
	return drainSwitchMin(info, s, k, header)
}

func drainSwitchConst(info *types.Info, s *ast.SwitchStmt, k flow.Key, header int64, v string) (int64, bool) {
	best, found, hasDefault, ok := switchConstLoop(info, s, k, v)
	if !ok {
		return 0, false
	}
	if found {
		return header + best, true
	}
	if !hasDefault {
		return header, true
	}
	return drainSwitchDefault(info, s, k, header)
}

func switchConstLoop(info *types.Info, s *ast.SwitchStmt, k flow.Key, v string) (best int64, found, hasDefault, ok bool) {
	best = -1
	for _, c := range s.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return 0, false, false, false
		}
		if len(cc.List) == 0 {
			hasDefault = true
			continue
		}
		n, live, ok := switchConstClause(info, cc, k, v)
		if !ok {
			return 0, false, false, false
		}
		if !live {
			continue
		}
		if !found || n < best {
			best, found = n, true
		}
	}
	return best, found, hasDefault, true
}

func switchConstClause(info *types.Info, cc *ast.CaseClause, k flow.Key, v string) (n int64, live, ok bool) {
	if !caseMatches(info, cc.List, v) {
		return 0, false, true
	}
	n, ok = caseBodyRecvs(info, cc, k)
	if !ok {
		return 0, false, false
	}
	if clauseEndsNoReturn(info, cc.Body) {
		return 0, false, true
	}
	return n, true, true
}

func drainSwitchDefault(info *types.Info, s *ast.SwitchStmt, k flow.Key, header int64) (int64, bool) {
	n, ok := defaultCaseRecvs(info, s.Body, k)
	if !ok {
		return 0, false
	}
	if defaultEndsNoReturn(info, s.Body) {
		return 1 << 60, true
	}
	return header + n, true
}

func drainSwitchMin(info *types.Info, s *ast.SwitchStmt, k flow.Key, header int64) (int64, bool) {
	best, foundLive, hasDefault, ok := switchMinLoop(info, s, k)
	if !ok {
		return 0, false
	}
	if !hasDefault {
		return switchNoDefault(header, best, foundLive)
	}
	if !foundLive {
		return 1 << 60, true
	}
	return header + best, true
}

func switchMinLoop(info *types.Info, s *ast.SwitchStmt, k flow.Key) (best int64, foundLive, hasDefault, ok bool) {
	best = -1
	for _, c := range s.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return 0, false, false, false
		}
		if len(cc.List) == 0 {
			hasDefault = true
		}
		n, live, ok := switchLiveCount(info, cc, k)
		if !ok {
			return 0, false, false, false
		}
		if !live {
			continue
		}
		if !foundLive || n < best {
			best, foundLive = n, true
		}
	}
	return best, foundLive, hasDefault, true
}

func switchLiveCount(info *types.Info, cc *ast.CaseClause, k flow.Key) (n int64, live, ok bool) {
	n, ok = caseBodyRecvs(info, cc, k)
	if !ok {
		return 0, false, false
	}
	if clauseEndsNoReturn(info, cc.Body) {
		return 0, false, true
	}
	return n, true, true
}

func switchNoDefault(header, best int64, foundLive bool) (int64, bool) {
	if !foundLive {
		return header, true
	}
	if 0 < best {
		return header, true
	}
	return header + best, true
}

func drainTypeSwitchRecvs(info *types.Info, s *ast.TypeSwitchStmt, k flow.Key) (int64, bool) {
	header := stmtRecvs(info, s.Init, k) + stmtRecvs(info, s.Assign, k)
	if s.Body == nil {
		return header, true
	}
	best, foundLive, hasDefault, ok := typeSwitchMin(info, s, k)
	if !ok {
		return 0, false
	}
	if !hasDefault {
		return switchNoDefault(header, best, foundLive)
	}
	if !foundLive {
		return 1 << 60, true
	}
	return header + best, true
}

func typeSwitchMin(info *types.Info, s *ast.TypeSwitchStmt, k flow.Key) (best int64, foundLive, hasDefault, ok bool) {
	best = -1
	foundLive = false
	hasDefault = false
	for _, c := range s.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return 0, false, false, false
		}
		if len(cc.List) == 0 {
			hasDefault = true
		}
		n, ok := caseBodyRecvs(info, cc, k)
		if !ok {
			return 0, false, false, false
		}
		if clauseEndsNoReturn(info, cc.Body) {
			continue
		}
		if !foundLive || n < best {
			best, foundLive = n, true
		}
	}
	return best, foundLive, hasDefault, true
}

func drainSelectRecvs(info *types.Info, s *ast.SelectStmt, k flow.Key) (int64, bool) {
	if s.Body == nil {
		return 0, true
	}
	best, foundLive, hasDefault, ok := selectMin(info, s, k)
	if !ok {
		return 0, false
	}
	if !hasDefault {
		if !foundLive {
			return 1 << 60, true
		}
		return best, true
	}
	if !foundLive {
		return 1 << 60, true
	}
	if 0 < best {
		return 0, true
	}
	return best, true
}

func selectMin(info *types.Info, s *ast.SelectStmt, k flow.Key) (best int64, foundLive, hasDefault, ok bool) {
	best = -1
	foundLive = false
	hasDefault = false
	for _, c := range s.Body.List {
		cc, ok := c.(*ast.CommClause)
		if !ok {
			return 0, false, false, false
		}
		n, ok := selectCaseCount(info, cc, k, &hasDefault)
		if !ok {
			return 0, false, false, false
		}
		if clauseEndsNoReturn(info, cc.Body) {
			continue
		}
		if !foundLive || n < best {
			best, foundLive = n, true
		}
	}
	return best, foundLive, hasDefault, true
}

func selectCaseCount(info *types.Info, cc *ast.CommClause, k flow.Key, hasDefault *bool) (int64, bool) {
	if cc.Comm == nil {
		*hasDefault = true
		return clauseBodyRecvs(info, cc.Body, k)
	}
	return selectCaseRecvs(info, cc, k)
}

func caseBodyRecvs(info *types.Info, cc *ast.CaseClause, k flow.Key) (int64, bool) {
	n, ok := clauseBodyRecvs(info, cc.Body, k)
	if !ok {
		return 0, false
	}
	n += caseListRecvs(info, cc.List, k)
	if hasUncountedRecv(info, cc, k, n) {
		return 0, false
	}
	return n, true
}

func defaultCaseRecvs(info *types.Info, body *ast.BlockStmt, k flow.Key) (int64, bool) {
	for _, c := range body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return 0, false
		}
		if len(cc.List) != 0 {
			continue
		}
		return caseBodyRecvs(info, cc, k)
	}
	return 0, true
}

func clauseBodyRecvs(info *types.Info, list []ast.Stmt, k flow.Key) (int64, bool) {
	var n int64
	for _, st := range list {
		if !isStraightStmt(st) {
			return 0, false
		}
		n += recvCount(info, st, k)
	}
	return n, true
}

func selectCaseRecvs(info *types.Info, cc *ast.CommClause, k flow.Key) (int64, bool) {
	var n int64
	if cc.Comm != nil {
		n += commRecvs(info, cc.Comm, k)
	}
	m, ok := clauseBodyRecvs(info, cc.Body, k)
	if !ok {
		return 0, false
	}
	n += m
	if hasUncountedRecv(info, cc, k, n) {
		return 0, false
	}
	return n, true
}

func commRecvs(info *types.Info, s ast.Stmt, k flow.Key) int64 {
	w := &opWalker{info: info, key: k}
	w.stmt(s)
	var n int64
	for _, op := range w.out {
		if op.Kind == opRecv {
			n++
		}
	}
	return n
}

func caseListRecvs(info *types.Info, list []ast.Expr, k flow.Key) int64 {
	var n int64
	for _, e := range list {
		n += uncondRecvs(info, e, k)
	}
	return n
}

func hasUncountedRecv(info *types.Info, n ast.Node, k flow.Key, counted int64) bool {
	return totalNodeRecvs(info, n, k) != counted
}
