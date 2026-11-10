package lockstate

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

func runDeferloop(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkDeferloop(pass, cur.Node())
	}
	return nil, nil
}

func checkDeferloop(pass *analysis.Pass, fn ast.Node) {
	checkLoopList(pass, loopBodies(fn))
}

type loopScope struct {
	loop ast.Node
	list []ast.Stmt
}

func loopBodies(fn ast.Node) []loopScope {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	var out []loopScope
	ast.Inspect(fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		if f, ok := n.(*ast.ForStmt); ok {
			out = append(out, loopScope{loop: n, list: f.Body.List})
		}
		if r, ok := n.(*ast.RangeStmt); ok {
			out = append(out, loopScope{loop: n, list: r.Body.List})
		}
		return true
	})
	return out
}

func checkLoopList(pass *analysis.Pass, scopes []loopScope) {
	for _, sc := range scopes {
		if len(sc.list) == 0 || lastIsExit(sc.list) {
			continue
		}
		for i, st := range sc.list {
			checkLoopDefer(pass, sc, i, st)
		}
	}
}

func lastIsExit(list []ast.Stmt) bool {
	last := list[len(list)-1]
	if _, ok := last.(*ast.ReturnStmt); ok {
		return true
	}
	if b, ok := last.(*ast.BranchStmt); ok && b.Tok == token.BREAK {
		return true
	}
	return false
}

func checkLoopDefer(pass *analysis.Pass, sc loopScope, i int, st ast.Stmt) {
	ds, ok := st.(*ast.DeferStmt)
	if !ok {
		return
	}
	dk, dm, ok := mutexKey(pass.TypesInfo, ds.Call)
	if !ok {
		return
	}
	want := matchLock(dm)
	if want == "" {
		return
	}
	if rootInLoop(pass.TypesInfo, dk, sc) {
		return
	}
	if !loopHasLock(pass, sc.list, i, dk, want) {
		return
	}
	recv := recvText(pass.TypesInfo, ds.Call)
	pass.Reportf(ds.Call.Pos(), "the deferred unlock runs at function exit, so the second iteration locks %s while it is held: unlock at the end of each iteration", recv)
}

func rootInLoop(info *types.Info, key flow.Key, sc loopScope) bool {
	pos := key.Root.Pos()
	if pos.IsValid() {
		for _, st := range sc.list {
			if pos >= st.Pos() && pos <= st.End() {
				return true
			}
		}
	}
	return headerDeclares(info, sc.loop, key)
}

func headerDeclares(info *types.Info, loop ast.Node, key flow.Key) bool {
	switch n := loop.(type) {
	case *ast.RangeStmt:
		return declaresRoot(info, n.Key, key) || declaresRoot(info, n.Value, key)
	case *ast.ForStmt:
		return initDeclaresRoot(info, n.Init, key)
	}
	return false
}

func declaresRoot(info *types.Info, e ast.Expr, key flow.Key) bool {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		return false
	}
	if v, ok := info.Defs[id]; ok {
		return v == key.Root
	}
	return info.Uses[id] == key.Root
}

func initDeclaresRoot(info *types.Info, init ast.Stmt, key flow.Key) bool {
	switch n := init.(type) {
	case *ast.AssignStmt:
		return lhsDeclaresRoot(info, n.Lhs, key)
	case *ast.DeclStmt:
		return declDeclaresRoot(info, n.Decl, key)
	}
	return false
}

func lhsDeclaresRoot(info *types.Info, lhs []ast.Expr, key flow.Key) bool {
	for _, e := range lhs {
		if declaresRoot(info, e, key) {
			return true
		}
	}
	return false
}

func declDeclaresRoot(info *types.Info, d ast.Decl, key flow.Key) bool {
	gd, ok := d.(*ast.GenDecl)
	if !ok {
		return false
	}
	for _, spec := range gd.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok && lhsDeclaresRoot(info, identExprs(vs.Names), key) {
			return true
		}
	}
	return false
}

func identExprs(ids []*ast.Ident) []ast.Expr {
	out := make([]ast.Expr, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out
}

func matchLock(deferMethod string) string {
	if deferMethod == methodUnlock {
		return methodLock
	}
	if deferMethod == methodRUnlock {
		return methodRLock
	}
	return ""
}

func loopHasLock(pass *analysis.Pass, list []ast.Stmt, upto int, key flow.Key, want string) bool {
	for _, st := range list[:upto] {
		if stmtIsLock(pass, st, key, want) {
			return true
		}
	}
	return false
}

func stmtIsLock(pass *analysis.Pass, st ast.Stmt, key flow.Key, want string) bool {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return false
	}
	k, m, ok := mutexKey(pass.TypesInfo, call)
	return ok && k == key && m == want
}

func runTrylock(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkTrylock(pass, cur.Node())
	}
	return nil, nil
}

func checkTrylock(pass *analysis.Pass, fn ast.Node) {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		switch n := n.(type) {
		case *ast.ExprStmt:
			checkTryExpr(pass, n)
		case *ast.AssignStmt:
			checkTryAssign(pass, n)
		}
		return true
	})
}

func checkTryExpr(pass *analysis.Pass, es *ast.ExprStmt) {
	call, m, recv, ok := tryCall(pass.TypesInfo, es.X)
	if !ok {
		return
	}
	pass.Reportf(call.Pos(), "the result of %s.%s is dropped, so the code below runs whether or not the lock was taken: branch on the result, or call %s.%s", recv, m, recv, lockForTry(m))
}

func checkTryAssign(pass *analysis.Pass, as *ast.AssignStmt) {
	if len(as.Rhs) != 1 || !allBlank(as.Lhs) {
		return
	}
	call, m, recv, ok := tryCall(pass.TypesInfo, as.Rhs[0])
	if !ok {
		return
	}
	pass.Reportf(call.Pos(), "the result of %s.%s is dropped, so the code below runs whether or not the lock was taken: branch on the result, or call %s.%s", recv, m, recv, lockForTry(m))
}

func allBlank(lhs []ast.Expr) bool {
	for _, e := range lhs {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok || id.Name != "_" {
			return false
		}
	}
	return len(lhs) > 0
}

func tryCall(info *types.Info, e ast.Expr) (call *ast.CallExpr, method, recv string, ok bool) {
	call, ok = ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, "", "", false
	}
	var m string
	var k flow.Key
	k, m, ok = mutexKey(info, call)
	if !ok || k.Root == nil {
		return nil, "", "", false
	}
	if m != methodTryLock && m != methodTryRLock {
		return nil, "", "", false
	}
	return call, m, recvText(info, call), true
}

func lockForTry(m string) string {
	if m == methodTryRLock {
		return methodRLock
	}
	return methodLock
}

func runSelfcopy(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil)) {
		checkSelfcopy(pass, cur.Node())
	}
	return nil, nil
}

func checkSelfcopy(pass *analysis.Pass, fn ast.Node) {
	fd, ok := fn.(*ast.FuncDecl)
	if !ok {
		return
	}
	rv, ok := valueRecv(pass.TypesInfo, fd)
	if !ok {
		return
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		checkSelfcopyCall(pass, rv, call)
		return true
	})
}

func valueRecv(info *types.Info, fd *ast.FuncDecl) (*types.Var, bool) {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return nil, false
	}
	f := fd.Recv.List[0]
	if _, ok := f.Type.(*ast.StarExpr); ok {
		return nil, false
	}
	if len(f.Names) == 0 {
		return nil, false
	}
	v, ok := info.Defs[f.Names[0]].(*types.Var)
	if !ok {
		return nil, false
	}
	return v, true
}

func checkSelfcopyCall(pass *analysis.Pass, rv *types.Var, call *ast.CallExpr) {
	k, m, ok := mutexKey(pass.TypesInfo, call)
	if !ok {
		return
	}
	if m != methodLock && m != methodRLock {
		return
	}
	if k.Root != rv {
		return
	}
	if pathHasPointer(rv.Type(), k.Path) {
		return
	}
	recv := recvText(pass.TypesInfo, call)
	pass.Reportf(call.Pos(), "this method has a value receiver, so it locks a copy of %s and excludes nothing: use a pointer receiver", recv)
}

func pathHasPointer(root types.Type, path string) bool {
	cur := root
	for part := range strings.SplitSeq(path, ".") {
		if part == "" {
			continue
		}
		ft, ok := fieldType(cur, part)
		if !ok {
			return true
		}
		if _, ok := ft.Underlying().(*types.Pointer); ok {
			return true
		}
		cur = ft
	}
	return false
}

func fieldType(cur types.Type, name string) (types.Type, bool) {
	st, ok := cur.Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}
	for i := range st.NumFields() {
		if st.Field(i).Name() == name {
			return st.Field(i).Type(), true
		}
	}
	return nil, false
}
