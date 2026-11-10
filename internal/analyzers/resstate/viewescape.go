package resstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

const (
	viewBufioPath     = "bufio"
	viewBytesPath     = "bytes"
	viewScannerName   = "Scanner"
	viewReaderName    = "Reader"
	viewBufferName    = "Buffer"
	viewBytesName     = "Bytes"
	viewPeekName      = "Peek"
	viewReadSliceName = "ReadSlice"
)

func runViewescape(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		viewFunc(pass, cur.Node())
	}
	return nil, nil
}

func viewFunc(pass *analysis.Pass, fn ast.Node) {
	seen := map[token.Pos]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if n != fn {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
		}
		loop, ok := loopBody(n)
		if !ok {
			return true
		}
		viewLoop(pass, fn, n, loop, seen)
		return true
	})
}

func loopBody(n ast.Node) (ast.Node, bool) {
	switch x := n.(type) {
	case *ast.ForStmt:
		return x.Body, true
	case *ast.RangeStmt:
		return x.Body, true
	}
	return nil, false
}

func viewLoop(pass *analysis.Pass, fn, loop, body ast.Node, seen map[token.Pos]bool) {
	invalid := invalidReceivers(pass, loop)
	if len(invalid) == 0 {
		return
	}
	views := viewDefs(pass, fn)
	sinks := sinkExprs(pass, loop, body)
	for _, s := range sinks {
		v, recvExpr, viewMethod, ok := sinkView(pass, s, views)
		if !ok {
			continue
		}
		recvKey, ok := flow.KeyOf(pass.TypesInfo, recvExpr)
		if !ok {
			continue
		}
		method, ok := invalidOther(invalid[recvKey], viewMethod)
		if !ok {
			continue
		}
		if seen[s.Pos()] {
			continue
		}
		seen[s.Pos()] = true
		pass.Reportf(s.Pos(), "%s points into the buffer of %s, and the next %s call overwrites it: store a copy made with bytes.Clone", types.ExprString(v), types.ExprString(recvExpr), method)
	}
}

func invalidOther(methods []string, viewMethod string) (string, bool) {
	for _, m := range methods {
		if m != viewMethod {
			return m, true
		}
	}
	return "", false
}

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
	for _, m := range out[k] {
		if m == name {
			return
		}
	}
	out[k] = append(out[k], name)
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
	return sel.Sel.Name
}

func invalidMethod(name string) bool {
	switch name {
	case "Scan", "Reset", "Write", "WriteString", "Read", viewReadSliceName, viewPeekName, "Discard":
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
		if !ok || as.Tok != token.DEFINE {
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
		return
	}
	recv, method, ok := viewCallOf(pass, rhs)
	if !ok {
		return
	}
	out[v] = viewSrc{recv: recv, method: method}
	defPos[v] = pos
}

func viewReassigned(pass *analysis.Pass, fn ast.Node, v *types.Var, after token.Pos) bool {
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

func viewReassignsAt(pass *analysis.Pass, n ast.Node, v *types.Var, after token.Pos) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return x.Pos() > after && viewAssigns(pass, x.Lhs, v)
	case *ast.IncDecStmt:
		return x.Pos() > after && viewTargetIs(pass, x.X, v)
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

func sinkExprs(pass *analysis.Pass, loop, body ast.Node) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch x := n.(type) {
		case *ast.CallExpr:
			if isAppendCall(pass, x) {
				out = appendElemArgs(x, out)
			}
		case *ast.SendStmt:
			out = append(out, x.Value)
		case *ast.AssignStmt:
			out = appendAssignRHS(pass, loop, x, out)
		case *ast.ReturnStmt:
			out = append(out, x.Results...)
		}
		return true
	})
	return out
}

func isAppendCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "append" {
		return false
	}
	obj, ok := pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok && obj.Name() == "append"
}

func appendElemArgs(call *ast.CallExpr, out []ast.Expr) []ast.Expr {
	if len(call.Args) < 2 {
		return out
	}
	if call.Ellipsis.IsValid() {
		return out
	}
	return append(out, call.Args[1:]...)
}

func appendAssignRHS(pass *analysis.Pass, loop ast.Node, as *ast.AssignStmt, out []ast.Expr) []ast.Expr {
	if len(as.Rhs) == 1 && len(as.Lhs) > 1 {
		for _, lhs := range as.Lhs {
			if fieldOrMapTarget(pass, lhs) || outsideVar(pass, loop, lhs) {
				return append(out, as.Rhs[0])
			}
		}
		return out
	}
	for i, lhs := range as.Lhs {
		if fieldOrMapTarget(pass, lhs) {
			out = appendRHS(as, i, out)
			continue
		}
		if outsideVar(pass, loop, lhs) {
			out = appendRHS(as, i, out)
		}
	}
	return out
}

func appendRHS(as *ast.AssignStmt, i int, out []ast.Expr) []ast.Expr {
	if len(as.Rhs) == 1 && len(as.Lhs) > 1 {
		return append(out, as.Rhs[0])
	}
	if len(as.Rhs) == len(as.Lhs) {
		return append(out, as.Rhs[i])
	}
	if len(as.Rhs) == 1 {
		return append(out, as.Rhs[0])
	}
	return out
}

func fieldOrMapTarget(pass *analysis.Pass, lhs ast.Expr) bool {
	switch x := ast.Unparen(lhs).(type) {
	case *ast.SelectorExpr:
		return true
	case *ast.IndexExpr:
		return viewIndexIsMap(pass, x.X)
	}
	return false
}

func viewIndexIsMap(pass *analysis.Pass, x ast.Expr) bool {
	t := pass.TypesInfo.Types[ast.Unparen(x)].Type
	if t == nil {
		return false
	}
	if m, ok := types.Unalias(t).Underlying().(*types.Map); ok && m != nil {
		return true
	}
	return false
}

func outsideVar(pass *analysis.Pass, loop ast.Node, lhs ast.Expr) bool {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok || id.Name == "_" {
		return false
	}
	v := viewVarOf(pass, id)
	if v == nil {
		return false
	}
	if v.Pos() >= loop.Pos() && v.Pos() <= loop.End() {
		return false
	}
	return true
}

func viewVarOf(pass *analysis.Pass, id *ast.Ident) *types.Var {
	if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok && v != nil {
		return v
	}
	if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
		return v
	}
	return nil
}

func sinkView(pass *analysis.Pass, s ast.Expr, views map[*types.Var]viewSrc) (view, recv ast.Expr, method string, found bool) {
	inner := ast.Unparen(s)
	if rv, name, ok := viewCallOf(pass, inner); ok {
		return inner, rv, name, true
	}
	id, ok := ast.Unparen(s).(*ast.Ident)
	if !ok {
		return nil, nil, "", false
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return nil, nil, "", false
	}
	src, found := views[v]
	if !found {
		return nil, nil, "", false
	}
	return id, src.recv, src.method, true
}
