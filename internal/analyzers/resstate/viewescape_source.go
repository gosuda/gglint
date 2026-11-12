package resstate

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func invalidReceivers(pass *analysis.Pass, loop ast.Node) map[flow.Key][]string {
	out := map[flow.Key][]string{}
	for _, part := range invalidParts(loop) {
		invalidCollect(pass, part, out)
	}
	return out
}

func invalidCollect(pass *analysis.Pass, part ast.Node, out map[flow.Key][]string) {
	ast.Inspect(part, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		invalidAdd(pass, call, out)
		return true
	})
}

func invalidAdd(pass *analysis.Pass, call *ast.CallExpr, out map[flow.Key][]string) {
	name := viewInvalidName(pass, call)
	if name == "" {
		return
	}
	k, ok := flow.MethodKey(pass.TypesInfo, call)
	if !ok {
		return
	}
	if !slices.Contains(out[k], name) {
		out[k] = append(out[k], name)
	}
}

func invalidParts(loop ast.Node) []ast.Node {
	switch x := loop.(type) {
	case *ast.ForStmt:
		parts := []ast.Node{}
		if x.Cond != nil {
			parts = append(parts, x.Cond)
		}
		if x.Body != nil {
			parts = append(parts, x.Body)
		}
		if x.Post != nil {
			parts = append(parts, x.Post)
		}
		return parts
	case *ast.RangeStmt:
		parts := []ast.Node{}
		if x.X != nil {
			parts = append(parts, x.X)
		}
		if x.Body != nil {
			parts = append(parts, x.Body)
		}
		return parts
	}
	return []ast.Node{loop}
}

func viewInvalidName(pass *analysis.Pass, call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || !invalidMethod(sel.Sel.Name) {
		return ""
	}
	obj := typeutil.StaticCallee(pass.TypesInfo, call)
	if obj == nil {
		return ""
	}
	recv := obj.Signature().Recv()
	if recv == nil {
		return ""
	}
	if !viewInvalidRecv(recv.Type(), sel.Sel.Name) {
		return ""
	}
	return sel.Sel.Name
}

func viewInvalidRecv(t types.Type, name string) bool {
	named, ok := derefNamed(t)
	if !ok {
		return false
	}
	path, recv, ok := pkgRecv(named)
	if !ok {
		return false
	}
	for _, want := range viewInvalidRecvTable(name) {
		if path == want.path && recv == want.recv {
			return true
		}
	}
	return false
}

type viewRecv struct {
	path string
	recv string
}

func viewInvalidRecvTable(name string) []viewRecv {
	scanner := viewRecv{viewBufioPath, viewScannerName}
	reader := viewRecv{viewBufioPath, viewReaderName}
	buffer := viewRecv{viewBytesPath, viewBufferName}
	switch name {
	case viewScanName:
		return []viewRecv{scanner}
	case viewResetName:
		return []viewRecv{buffer}
	case closeMethodWrite, viewWriteStringName:
		return []viewRecv{buffer}
	case closeMethodRead:
		return []viewRecv{reader}
	case viewReadSliceName, viewPeekName, viewDiscardName:
		return []viewRecv{reader}
	}
	return nil
}

var viewMethodAdvances = map[string]bool{
	viewReadSliceName: true,
}

func invalidMethod(name string) bool {
	switch name {
	case viewScanName, viewResetName, closeMethodWrite, viewWriteStringName, closeMethodRead, viewReadSliceName, viewPeekName, viewDiscardName:
		return true
	}
	return false
}

func derefNamed(t types.Type) (*types.Named, bool) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	return named, ok
}

func pkgRecv(named *types.Named) (path, recv string, ok bool) {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return "", "", false
	}
	return obj.Pkg().Path(), obj.Name(), true
}

func matchViewSource(path, recv, name string) bool {
	switch name {
	case viewBytesName:
		return isBufioScanner(path, recv) || (path == viewBytesPath && recv == viewBufferName)
	case viewPeekName, viewReadSliceName:
		return isBufioReader(path, recv)
	}
	return false
}

func isBufioScanner(path, recv string) bool {
	return path == viewBufioPath && recv == viewScannerName
}

func isBufioReader(path, recv string) bool {
	return path == viewBufioPath && recv == viewReaderName
}

type viewSrc struct {
	recv   ast.Expr
	method string
}

func viewDefs(pass *analysis.Pass, fn ast.Node) map[*types.Var]viewSrc {
	out := map[*types.Var]viewSrc{}
	defPos := map[*types.Var]token.Pos{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || (as.Tok != token.DEFINE && as.Tok != token.ASSIGN) {
			return true
		}
		viewDefineAdd(pass, out, defPos, as)
		return true
	})
	for v, pos := range defPos {
		if viewReassigned(pass, fn, v, pos) {
			delete(out, v)
		}
	}
	return out
}

func viewDefineAdd(pass *analysis.Pass, out map[*types.Var]viewSrc, defPos map[*types.Var]token.Pos, as *ast.AssignStmt) {
	if len(as.Lhs) == 1 && len(as.Rhs) == 1 {
		viewDefineOne(pass, out, defPos, as.Lhs[0], as.Rhs[0], as.Pos())
		return
	}
	if len(as.Rhs) == 1 && len(as.Lhs) == 2 {
		viewDefineOne(pass, out, defPos, as.Lhs[0], as.Rhs[0], as.Pos())
	}
}

func viewDefineOne(pass *analysis.Pass, out map[*types.Var]viewSrc, defPos map[*types.Var]token.Pos, lhs, rhs ast.Expr, pos token.Pos) {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.Defs[id].(*types.Var)
	if !ok {
		v, ok = pass.TypesInfo.Uses[id].(*types.Var)
		if !ok {
			return
		}
	}
	recv, method, ok := viewCallOf(pass, rhs)
	if !ok {
		return
	}
	out[v] = viewSrc{recv: recv, method: method}
	defPos[v] = pos
}

func viewReassigned(pass *analysis.Pass, fn ast.Node, v *types.Var, after token.Pos) bool {
	if litReassignsView(pass, fn, v) {
		return true
	}
	reassigned := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if reassigned {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		if viewReassignsAt(pass, n, v, after) {
			reassigned = true
		}
		return !reassigned
	})
	return reassigned
}

func litReassignsView(pass *analysis.Pass, fn ast.Node, v *types.Var) bool {
	assigned := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if assigned {
			return false
		}
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit == fn {
			return true
		}
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			if assigned {
				return false
			}
			if viewAssignsNode(pass, m, v) {
				assigned = true
				return false
			}
			return true
		})
		return !assigned
	})
	return assigned
}

func viewAssignsNode(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return viewAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return viewTargetIs(pass, x.X, v)
	}
	return false
}

func viewReassignsAt(pass *analysis.Pass, n ast.Node, v *types.Var, after token.Pos) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return x.Pos() > after && viewAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return x.Pos() > after && viewTargetIs(pass, x.X, v)
	}
	return false
}

func viewRangeBinds(pass *analysis.Pass, x *ast.RangeStmt, v *types.Var) bool {
	for _, e := range []ast.Expr{x.Key, x.Value} {
		if viewTargetIs(pass, e, v) {
			return true
		}
	}
	return false
}

func viewAssigns(pass *analysis.Pass, lhs []ast.Expr, v *types.Var) bool {
	for _, l := range lhs {
		if viewTargetIs(pass, l, v) {
			return true
		}
	}
	return false
}

func viewTargetIs(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	return pass.TypesInfo.Uses[id] == v || pass.TypesInfo.Defs[id] == v
}

func viewCallOf(pass *analysis.Pass, e ast.Expr) (ast.Expr, string, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	return viewCallRecv(pass, call)
}

func viewCallRecv(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, string, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, "", false
	}
	switch sel.Sel.Name {
	case viewBytesName, viewPeekName, viewReadSliceName:
	default:
		return nil, "", false
	}
	obj := typeutil.StaticCallee(pass.TypesInfo, call)
	if obj == nil {
		return nil, "", false
	}
	recv := obj.Signature().Recv()
	if recv == nil {
		return nil, "", false
	}
	if !isViewSourceType(recv.Type(), sel.Sel.Name) {
		return nil, "", false
	}
	return sel.X, sel.Sel.Name, true
}

func isViewSourceType(t types.Type, name string) bool {
	named, ok := derefNamed(t)
	if !ok {
		return false
	}
	path, recv, ok := pkgRecv(named)
	if !ok {
		return false
	}
	return matchViewSource(path, recv, name)
}
