package chanstate

import (
	"go/ast"
	"go/token"

	"github.com/gosuda/gglint/internal/flow"
)

func loopExits(fn ast.Node, r *ast.RangeStmt) bool {
	own, outer := rangeLabelSets(fn, r)
	return exitsList(r.Body.List, 0, own, outer)
}

func rangeLabelSets(fn ast.Node, target *ast.RangeStmt) (own, outer map[string]bool) {
	own = map[string]bool{}
	outer = map[string]bool{}
	body := flow.Body(fn)
	if body == nil {
		return own, outer
	}
	path, ok := findRangeListPath(body.List, target, []ast.Node{body})
	if !ok {
		return own, outer
	}
	split := len(path) - 1
	for split > 0 {
		if _, ok := path[split-1].(*ast.LabeledStmt); !ok {
			break
		}
		split--
	}
	for _, n := range path[split : len(path)-1] {
		if l, ok := n.(*ast.LabeledStmt); ok {
			own[l.Label.Name] = true
		}
	}
	for _, n := range path[:split] {
		if l, ok := n.(*ast.LabeledStmt); ok {
			outer[l.Label.Name] = true
		}
	}
	return own, outer
}

func findRangeListPath(list []ast.Stmt, target *ast.RangeStmt, stack []ast.Node) ([]ast.Node, bool) {
	for _, s := range list {
		if r, ok := s.(*ast.RangeStmt); ok && r == target {
			return append(stack, s), true
		}
		if path, ok := findRangeStmtPath(s, target, append(stack, s)); ok {
			return path, true
		}
	}
	return nil, false
}

func findRangeStmtPath(s ast.Stmt, target *ast.RangeStmt, stack []ast.Node) ([]ast.Node, bool) {
	switch s := s.(type) {
	case *ast.LabeledStmt:
		return findRangeLabeledPath(s, target, stack)
	case *ast.BlockStmt:
		return findRangeListPath(s.List, target, stack)
	case *ast.IfStmt:
		return findRangeIfPath(s, target, stack)
	case *ast.ForStmt:
		if s.Body == nil {
			return nil, false
		}
		return findRangeStmtPath(s.Body, target, append(stack, s.Body))
	case *ast.RangeStmt:
		if s.Body == nil {
			return nil, false
		}
		return findRangeStmtPath(s.Body, target, append(stack, s.Body))
	default:
		return findRangeStructuredPath(s, target, stack)
	}
}

func findRangeLabeledPath(s *ast.LabeledStmt, target *ast.RangeStmt, stack []ast.Node) ([]ast.Node, bool) {
	if r, ok := s.Stmt.(*ast.RangeStmt); ok && r == target {
		return append(stack, s.Stmt), true
	}
	if s.Stmt == nil {
		return nil, false
	}
	return findRangeStmtPath(s.Stmt, target, append(stack, s.Stmt))
}

func findRangeIfPath(s *ast.IfStmt, target *ast.RangeStmt, stack []ast.Node) ([]ast.Node, bool) {
	if path, ok := findRangeStmtPath(s.Body, target, append(stack, s.Body)); ok {
		return path, true
	}
	if s.Else == nil {
		return nil, false
	}
	return findRangeStmtPath(s.Else, target, append(stack, s.Else))
}

func findRangeStructuredPath(s ast.Stmt, target *ast.RangeStmt, stack []ast.Node) ([]ast.Node, bool) {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		if s.Body == nil {
			return nil, false
		}
		return findRangeStmtPath(s.Body, target, append(stack, s.Body))
	case *ast.TypeSwitchStmt:
		if s.Body == nil {
			return nil, false
		}
		return findRangeStmtPath(s.Body, target, append(stack, s.Body))
	case *ast.SelectStmt:
		if s.Body == nil {
			return nil, false
		}
		return findRangeStmtPath(s.Body, target, append(stack, s.Body))
	case *ast.CaseClause:
		return findRangeListPath(s.Body, target, stack)
	case *ast.CommClause:
		return findRangeListPath(s.Body, target, stack)
	}
	return nil, false
}

func exitsList(l []ast.Stmt, depth int, own, outer map[string]bool) bool {
	for _, s := range l {
		if exitsStmt(s, depth, own, outer) {
			return true
		}
	}
	return false
}

func exitsStmt(s ast.Stmt, depth int, own, outer map[string]bool) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return exitsBranch(s, depth, own, outer)
	case *ast.BlockStmt:
		return exitsList(s.List, depth, own, outer)
	case *ast.LabeledStmt:
		return exitsStmt(s.Stmt, depth, own, outer)
	case *ast.IfStmt:
		return exitsIf(s, depth, own, outer)
	case *ast.ForStmt:
		return exitsFor(s, depth, own, outer)
	case *ast.RangeStmt:
		return exitsList(s.Body.List, depth+1, own, outer)
	default:
		return exitsStructured(s, depth, own, outer)
	}
}

func exitsBranch(s *ast.BranchStmt, depth int, own, outer map[string]bool) bool {
	if s.Tok == token.GOTO {
		return true
	}
	if s.Label == nil {
		return s.Tok == token.BREAK && depth == 0
	}
	return exitsLabeled(s, own, outer)
}

func exitsLabeled(s *ast.BranchStmt, own, outer map[string]bool) bool {
	name := s.Label.Name
	if s.Tok == token.BREAK {
		return own[name] || outer[name]
	}
	if s.Tok == token.CONTINUE {
		if own[name] {
			return false
		}
		return outer[name]
	}
	return false
}

func exitsIf(s *ast.IfStmt, depth int, own, outer map[string]bool) bool {
	if exitsOpt(s.Init, depth, own, outer) {
		return true
	}
	if exitsStmt(s.Body, depth, own, outer) {
		return true
	}
	return exitsOpt(s.Else, depth, own, outer)
}

func exitsFor(s *ast.ForStmt, depth int, own, outer map[string]bool) bool {
	if exitsOpt(s.Init, depth, own, outer) {
		return true
	}
	return exitsList(s.Body.List, depth+1, own, outer)
}

func exitsStructured(s ast.Stmt, depth int, own, outer map[string]bool) bool {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		return exitsOpt(s.Init, depth, own, outer) || exitsCases(s.Body.List, depth+1, own, outer)
	case *ast.TypeSwitchStmt:
		return exitsOpt(s.Init, depth, own, outer) || exitsCases(s.Body.List, depth+1, own, outer)
	case *ast.SelectStmt:
		return exitsCases(s.Body.List, depth+1, own, outer)
	case *ast.CaseClause:
		return exitsList(s.Body, depth, own, outer)
	case *ast.CommClause:
		return exitsList(s.Body, depth, own, outer)
	}
	return false
}

func exitsCases(l []ast.Stmt, depth int, own, outer map[string]bool) bool {
	return exitsList(l, depth, own, outer)
}

func exitsOpt(s ast.Stmt, depth int, own, outer map[string]bool) bool {
	return s != nil && exitsStmt(s, depth, own, outer)
}
