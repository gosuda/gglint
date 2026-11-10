package gorace

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

func runDoublecheck(pass *analysis.Pass) (any, error) {
	for _, cur := range funcDecls(pass) {
		checkDoublecheck(pass, inspection.As[*ast.FuncDecl](cur.Node()))
	}
	return nil, nil
}

func checkDoublecheck(pass *analysis.Pass, fn *ast.FuncDecl) {
	if passesMutex(pass.TypesInfo, fn) {
		return
	}
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	held := flow.HeldAt(pass, g)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Body != fn.Body {
			return false
		}
		if outer, ok := n.(*ast.IfStmt); ok {
			reportDoublecheck(pass, held, outer)
		}
		return true
	})
}

func reportDoublecheck(pass *analysis.Pass, held func(ast.Node) []flow.Key, outer *ast.IfStmt) {
	v, ok := nilKey(pass.TypesInfo, outer.Cond)
	if !ok || !doublecheckVar(pass, v) {
		return
	}
	if !innerAssign(pass.TypesInfo, outer.Body, v) {
		return
	}
	k, ok := lockInBody(pass.TypesInfo, outer.Body)
	if !ok || hasKey(held(outer), k) {
		return
	}
	pass.Reportf(outer.Cond.Pos(), "%s is read outside the lock and assigned inside it, so a reader can see a half-built value: read it under the lock, or use sync.Once", keyText(v))
}

func doublecheckVar(pass *analysis.Pass, v flow.Key) bool {
	if v.Root == nil {
		return false
	}
	if root, ok := v.Root.(*types.Var); ok && isSyncType(root.Type()) {
		return false
	}
	if v.Path != "" {
		return true
	}
	return isPackageVar(pass, mustVar(v))
}

func mustVar(k flow.Key) *types.Var {
	v, _ := k.Root.(*types.Var)
	return v
}

func hasKey(keys []flow.Key, k flow.Key) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
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

func innerAssign(info *types.Info, body *ast.BlockStmt, v flow.Key) bool {
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
		found = assignsPlain(info, inner.Body, v)
		return !found
	})
	return found
}

func assignsPlain(info *types.Info, body *ast.BlockStmt, v flow.Key) bool {
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
func lockInBody(info *types.Info, body *ast.BlockStmt) (flow.Key, bool) {
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
		if !ok {
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
	for _, cur := range funcDecls(pass) {
		checkSubtest(pass, inspection.As[*ast.FuncDecl](cur.Node()))
	}
	return nil, nil
}

type subtestRun struct {
	call     *ast.CallExpr
	lit      *ast.FuncLit
	parallel token.Pos
	acc      []varAccess
}

func checkSubtest(pass *analysis.Pass, fn *ast.FuncDecl) {
	for _, b := range blocksOf(fn) {
		checkSubtestBlock(pass, b)
	}
}

func blocksOf(fn *ast.FuncDecl) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if b, ok := n.(*ast.BlockStmt); ok {
			out = append(out, b)
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
				pass.Reportf(runs[i].call.Pos(), "parallel subtests write %s without ordering, so the test result depends on scheduling: give each subtest its own variable", keyText(k))
				break
			}
		}
	}
}

func subtestStmt(info *types.Info, stmt ast.Stmt) (subtestRun, bool) {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return subtestRun{}, false
	}
	call, ok := ast.Unparen(expr.X).(*ast.CallExpr)
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
	for _, w := range a.acc {
		if !w.write || w.index {
			continue
		}
		v, ok := w.key.Root.(types.Object)
		if !ok || !outsideLit(v, a.lit) || !outsideLit(v, b.lit) {
			continue
		}
		for _, r := range b.acc {
			if r.key == w.key {
				return w.key, true
			}
		}
	}
	return flow.Key{}, false
}
