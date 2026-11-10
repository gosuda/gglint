package alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func runRangemutate(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		seen := map[token.Pos]bool{}
		findRanges(fn, func(rs *ast.RangeStmt) {
			checkRangemutate(pass, fn, rs, seen)
		})
	})
	return nil, nil
}

func findRanges(fn ast.Node, fn2 func(*ast.RangeStmt)) {
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if rs, ok := n.(*ast.RangeStmt); ok {
			fn2(rs)
		}
		return true
	})
}

type rangeScope struct {
	key  flow.Key
	name string
	seen map[token.Pos]bool
}

type rangeScan struct {
	pass  *analysis.Pass
	scope rangeScope
	graph *flow.Graph
	entry ast.Stmt
}

func isSlice(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Slice)
	return ok
}

func checkRangemutate(pass *analysis.Pass, fn ast.Node, rs *ast.RangeStmt, seen map[token.Pos]bool) {
	info := pass.TypesInfo
	key, ok := flow.KeyOf(info, rs.X)
	if !ok {
		return
	}
	root, ok := key.Root.(*types.Var)
	if !ok || !flow.IsLocal(info, fn, root) {
		return
	}
	if !isSlice(info, rs.X) {
		return
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	entry := loopEntry(rs)
	if entry == nil {
		return
	}
	scan := &rangeScan{pass: pass, scope: rangeScope{key: key, name: types.ExprString(rs.X), seen: seen}, graph: g, entry: entry}
	scan.scanList(rs.Body.List)
}

func loopEntry(rs *ast.RangeStmt) ast.Stmt {
	if rs.Body == nil || len(rs.Body.List) == 0 {
		return nil
	}
	return rs.Body.List[0]
}

func (s *rangeScan) scanList(list []ast.Stmt) {
	for _, st := range list {
		if as, ok := st.(*ast.AssignStmt); ok {
			s.checkAppend(as)
		}
		for _, child := range childLists(st) {
			s.scanList(child)
		}
	}
}

func (s *rangeScan) checkAppend(st *ast.AssignStmt) {
	call, ok := rangeAppendCall(s.pass.TypesInfo, st, s.scope.key)
	if !ok {
		return
	}
	if !s.graph.Reaches(st, s.entry, nil) {
		return
	}
	if s.scope.seen[call.Pos()] {
		return
	}
	s.scope.seen[call.Pos()] = true
	s.pass.Reportf(call.Pos(), "appending to %s while ranging over it leaves the loop on the old length and can reallocate: collect into another slice", s.scope.name)
}

func rangeAppendCall(info *types.Info, st *ast.AssignStmt, key flow.Key) (*ast.CallExpr, bool) {
	if st.Tok != token.ASSIGN || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return nil, false
	}
	call, ok := isAppend(info, st.Rhs[0])
	if !ok || len(call.Args) < 2 {
		return nil, false
	}
	if !rangeAppendTargets(info, st, call, key) {
		return nil, false
	}
	return call, true
}

func rangeAppendTargets(info *types.Info, st *ast.AssignStmt, call *ast.CallExpr, key flow.Key) bool {
	first, ok := flow.KeyOf(info, call.Args[0])
	if !ok || first != key {
		return false
	}
	lhs, ok := flow.KeyOf(info, st.Lhs[0])
	return ok && lhs == key
}

func childLists(st ast.Stmt) [][]ast.Stmt {
	switch n := st.(type) {
	case *ast.BlockStmt:
		return [][]ast.Stmt{n.List}
	case *ast.LabeledStmt:
		if n.Stmt != nil {
			return [][]ast.Stmt{{n.Stmt}}
		}
	case *ast.IfStmt:
		return append([][]ast.Stmt{n.Body.List}, elseBranch(n.Else)...)
	case *ast.ForStmt, *ast.RangeStmt:
		return loopLists(st)
	case *ast.SwitchStmt:
		return blockClauses(n.Body)
	case *ast.TypeSwitchStmt:
		return blockClauses(n.Body)
	case *ast.SelectStmt:
		return blockClauses(n.Body)
	}
	return nil
}

func loopLists(st ast.Stmt) [][]ast.Stmt {
	switch n := st.(type) {
	case *ast.ForStmt:
		return [][]ast.Stmt{n.Body.List}
	case *ast.RangeStmt:
		return [][]ast.Stmt{n.Body.List}
	}
	return nil
}

func elseBranch(e ast.Stmt) [][]ast.Stmt {
	if b, ok := e.(*ast.BlockStmt); ok {
		return [][]ast.Stmt{b.List}
	}
	if s, ok := e.(*ast.IfStmt); ok {
		return append([][]ast.Stmt{s.Body.List}, elseBranch(s.Else)...)
	}
	return nil
}

func blockClauses(b *ast.BlockStmt) [][]ast.Stmt {
	var out [][]ast.Stmt
	if b == nil {
		return nil
	}
	for _, st := range b.List {
		switch n := st.(type) {
		case *ast.CaseClause:
			out = append(out, n.Body)
		case *ast.CommClause:
			out = append(out, n.Body)
		}
	}
	return out
}
