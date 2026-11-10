package gorace

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func runDoublecheck(pass *analysis.Pass) (any, error) {
	for _, fn := range funcBodies(pass) {
		checkDoublecheck(pass, fn)
	}
	return nil, nil
}

func checkDoublecheck(pass *analysis.Pass, fn ast.Node) {
	body := flow.Body(fn)
	if body == nil {
		return
	}
	if passesMutex(pass.TypesInfo, fn) {
		return
	}
	var outers []*ast.IfStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if outer, ok := n.(*ast.IfStmt); ok {
			outers = append(outers, outer)
		}
		return true
	})
	if len(outers) == 0 {
		return
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	held := flow.HeldAt(pass, g)
	for _, outer := range outers {
		reportDoublecheck(pass, fn, held, outer)
	}
}

func reportDoublecheck(pass *analysis.Pass, fn ast.Node, held func(ast.Node) []flow.Key, outer *ast.IfStmt) {
	v, ok := nilKey(pass.TypesInfo, outer.Cond)
	if !ok || !sharedVar(pass, fn, v) {
		return
	}
	innerPos, ok := innerAssignPos(pass.TypesInfo, outer.Body, v)
	if !ok {
		return
	}
	k, ok := lockBeforePos(pass.TypesInfo, outer.Body, innerPos)
	if !ok || hasKey(held(outer), k) {
		return
	}
	pass.Reportf(outer.Cond.Pos(), "%s is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once", keyText(pass, v))
}

func sharedVar(pass *analysis.Pass, fn ast.Node, v flow.Key) bool {
	if v.Root == nil {
		return false
	}
	if v.Path != "" {
		return !localRoot(pass, fn, v)
	}
	if vv, ok := v.Root.(*types.Var); ok && vv.Pkg() != nil && vv.Pkg().Scope() != nil {
		return vv.Pkg().Scope().Lookup(vv.Name()) == vv
	}
	return false
}

func localRoot(pass *analysis.Pass, fn ast.Node, v flow.Key) bool {
	vv, ok := v.Root.(*types.Var)
	if !ok {
		return false
	}
	return flow.IsLocal(pass.TypesInfo, fn, vv)
}

func hasKey(keys []flow.Key, k flow.Key) bool {
	return slices.Contains(keys, k)
}

func nilKey(info *types.Info, cond ast.Expr) (flow.Key, bool) {
	bin, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return flow.Key{}, false
	}
	if isNilExpr(info, bin.X) {
		return flow.KeyOf(info, bin.Y)
	}
	if isNilExpr(info, bin.Y) {
		return flow.KeyOf(info, bin.X)
	}
	return flow.Key{}, false
}

func isNilExpr(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
		return true
	}
	return info.Types[e].IsNil()
}

func innerAssignPos(info *types.Info, body *ast.BlockStmt, v flow.Key) (token.Pos, bool) {
	if body == nil {
		return token.NoPos, false
	}
	var pos token.Pos
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		inner, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		k, ok := nilKey(info, inner.Cond)
		if !ok || k != v {
			return true
		}
		if assignsPlain(info, inner.Body, v) {
			pos, found = inner.Pos(), true
			return false
		}
		return true
	})
	return pos, found
}

func assignsPlain(info *types.Info, body *ast.BlockStmt, v flow.Key) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		a, ok := n.(*ast.AssignStmt)
		if !ok || a.Tok != token.ASSIGN {
			return true
		}
		for _, lhs := range a.Lhs {
			if k, ok := flow.KeyOf(info, lhs); ok && k == v {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func lockBeforePos(info *types.Info, body *ast.BlockStmt, before token.Pos) (flow.Key, bool) {
	if body == nil {
		return flow.Key{}, false
	}
	var out flow.Key
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isMutexLock(lockName(info, call)) {
			return true
		}
		k, ok := flow.MethodKey(info, call)
		if !ok || call.Pos() >= before {
			return true
		}
		out, found = k, true
		return false
	})
	return out, found
}

func lockName(info *types.Info, call *ast.CallExpr) string {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return ""
	}
	return fn.FullName()
}

func runSubtest(pass *analysis.Pass) (any, error) {
	for _, fn := range funcBodies(pass) {
		checkSubtest(pass, fn)
	}
	return nil, nil
}

type subtestRun struct {
	call     *ast.CallExpr
	lit      *ast.FuncLit
	parallel token.Pos
	acc      []varAccess
}

func checkSubtest(pass *analysis.Pass, fn ast.Node) {
	for _, b := range blocksOf(fn) {
		checkSubtestBlock(pass, b)
	}
}

func blocksOf(fn ast.Node) []*ast.BlockStmt {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []*ast.BlockStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if b, ok := n.(*ast.BlockStmt); ok {
			out = append(out, b)
		}
		if c, ok := n.(*ast.CaseClause); ok {
			out = append(out, &ast.BlockStmt{List: c.Body})
		}
		if c, ok := n.(*ast.CommClause); ok {
			out = append(out, &ast.BlockStmt{List: c.Body})
		}
		return true
	})
	return out
}

func checkSubtestBlock(pass *analysis.Pass, b *ast.BlockStmt) {
	var runs []subtestRun
	for _, stmt := range b.List {
		if r, ok := subtestStmt(pass.TypesInfo, stmt); ok {
			runs = append(runs, r)
		}
	}
	for i := 1; i < len(runs); i++ {
		for j := range i {
			if k, ok := sharedSubtestVar(runs[i], runs[j]); ok {
				pass.Reportf(runs[i].call.Pos(), "parallel subtests write %s without ordering, so the test result depends on scheduling: give each subtest its own variable", keyText(pass, k))
				break
			}
		}
	}
}

func subtestStmt(info *types.Info, stmt ast.Stmt) (subtestRun, bool) {
	call, ok := findSubtestCall(info, stmt)
	if !ok || !isTRun(info, call) {
		return subtestRun{}, false
	}
	lit, ok := ast.Unparen(call.Args[1]).(*ast.FuncLit)
	if !ok {
		return subtestRun{}, false
	}
	pos, ok := parallelPos(info, lit)
	if !ok {
		return subtestRun{}, false
	}
	return subtestRun{call: call, lit: lit, parallel: pos, acc: accAfter(info, lit, pos)}, true
}

func findSubtestCall(info *types.Info, stmt ast.Stmt) (*ast.CallExpr, bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		c, ok := ast.Unparen(s.X).(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		return c, true
	case *ast.AssignStmt:
		for _, e := range s.Rhs {
			if c, ok := ast.Unparen(e).(*ast.CallExpr); ok && isTRun(info, c) {
				return c, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func isTRun(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" || len(call.Args) != 2 {
		return false
	}
	if _, ok := ast.Unparen(call.Args[1]).(*ast.FuncLit); !ok {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	return fn != nil && fn.FullName() == "(*testing.T).Run"
}

func parallelPos(info *types.Info, lit *ast.FuncLit) (token.Pos, bool) {
	if lit == nil || lit.Body == nil {
		return token.NoPos, false
	}
	var pos token.Pos
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if l, ok := n.(*ast.FuncLit); ok && l != lit {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isTParallel(info, call) {
			return true
		}
		pos, found = call.Pos(), true
		return false
	})
	return pos, found
}

func isTParallel(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Parallel" {
		return false
	}
	fn := typeutil.StaticCallee(info, call)
	return fn != nil && fn.FullName() == "(*testing.T).Parallel"
}

func accAfter(info *types.Info, lit *ast.FuncLit, pos token.Pos) []varAccess {
	var out []varAccess
	for _, a := range collectLit(info, lit) {
		if a.stmt.Pos() > pos {
			out = append(out, a)
		}
	}
	return out
}

func sharedSubtestVar(a, b subtestRun) (flow.Key, bool) {
	if k, ok := matchSubtestDir(a, b); ok {
		return k, true
	}
	return matchSubtestDir(b, a)
}

func matchSubtestDir(writes, reads subtestRun) (flow.Key, bool) {
	for _, w := range writes.acc {
		if !subtestWriteOutside(w, writes, reads) {
			continue
		}
		if k, ok := matchSubtestRead(w, reads.acc); ok {
			return k, true
		}
	}
	return flow.Key{}, false
}

func subtestWriteOutside(w varAccess, writes, reads subtestRun) bool {
	if !w.write || w.index {
		return false
	}
	return outsideLit(w.key.Root, writes.lit) && outsideLit(w.key.Root, reads.lit)
}

func matchSubtestRead(w varAccess, reads []varAccess) (flow.Key, bool) {
	for _, r := range reads {
		if subtestKeysOverlap(w.key, r.key) {
			return w.key, true
		}
	}
	return flow.Key{}, false
}

func subtestKeysOverlap(w, r flow.Key) bool {
	if r.Root != w.Root || r.Root == nil {
		return false
	}
	return r.Path == w.Path || r.Path == "" || w.Path == ""
}
