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
	scanRangeList(pass, rs.Body.List, nil, rangeScope{key: key, name: types.ExprString(rs.X), seen: seen})
}

func isSlice(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Slice)
	return ok
}

func scanRangeList(pass *analysis.Pass, list []ast.Stmt, after ast.Stmt, scope rangeScope) {
	for i, st := range list {
		next := after
		if i+1 < len(list) {
			next = list[i+1]
		}
		if as, ok := st.(*ast.AssignStmt); ok {
			checkRangeAppend(pass, as, next, scope)
		}
		for _, child := range childLists(st) {
			scanRangeList(pass, child, childAfter(st, next), scope)
		}
	}
}

func childAfter(st, next ast.Stmt) ast.Stmt {
	switch st.(type) {
	case *ast.BlockStmt, *ast.LabeledStmt:
		return next
	}
	return nil
}

func checkRangeAppend(pass *analysis.Pass, st *ast.AssignStmt, next ast.Stmt, scope rangeScope) {
	call, ok := rangeAppendCall(pass.TypesInfo, st, scope.key)
	if !ok {
		return
	}
	if next != nil && exitsLoop(next) {
		return
	}
	if scope.seen[call.Pos()] {
		return
	}
	scope.seen[call.Pos()] = true
	pass.Reportf(call.Pos(), "appending to %s while ranging over it leaves the loop on the old length and can reallocate: collect into another slice", scope.name)
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

func exitsLoop(st ast.Stmt) bool {
	if b, ok := st.(*ast.BranchStmt); ok {
		return b.Tok == token.BREAK
	}
	_, ok := st.(*ast.ReturnStmt)
	return ok
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
