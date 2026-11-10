package resstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

func runViewescape(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		viewFunc(pass, cur.Node())
	}
	return nil, nil
}

func viewFunc(pass *analysis.Pass, fn ast.Node) {
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
		viewLoop(pass, n, loop)
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

func viewLoop(pass *analysis.Pass, loop ast.Node, body ast.Node) {
	invalid := invalidReceivers(pass, loop)
	if len(invalid) == 0 {
		return
	}
	views := viewDefs(pass, body)
	sinks := sinkExprs(pass, body)
	for _, s := range sinks {
		v, recvExpr, ok := sinkView(pass, s, views)
		if !ok {
			continue
		}
		recvKey, ok := flow.KeyOf(pass.TypesInfo, recvExpr)
		if !ok {
			continue
		}
		method, ok := invalid[recvKey]
		if !ok {
			continue
		}
		pass.Reportf(s.Pos(), "%s points into the buffer of %s, and the next %s call overwrites it: store a copy made with bytes.Clone", types.ExprString(v), types.ExprString(recvExpr), method)
	}
}

func invalidReceivers(pass *analysis.Pass, loop ast.Node) map[flow.Key]string {
	out := map[flow.Key]string{}
	ast.Inspect(loop, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := viewInvalidName(pass, call)
		if name == "" {
			return true
		}
		k, ok := flow.MethodKey(pass.TypesInfo, call)
		if !ok {
			return true
		}
		if _, dup := out[k]; !dup {
			out[k] = name
		}
		return true
	})
	return out
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
	if isViewReceiver(recv.Type(), sel.Sel.Name) {
		return sel.Sel.Name
	}
	return ""
}

func invalidMethod(name string) bool {
	switch name {
	case "Scan", "Reset", "Write", "WriteString", "Read", "ReadSlice", "Peek", "Discard":
		return true
	}
	return false
}

func isViewReceiver(t types.Type, name string) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	path := obj.Pkg().Path()
	recv := obj.Name()
	switch name {
	case "Scan", "Bytes":
		return (path == "bufio" && recv == "Scanner") || (path == "bufio" && recv == "Reader")
	case "Peek", "ReadSlice", "Read", "Discard":
		return path == "bufio" && recv == "Reader"
	case "Write", "WriteString", "Reset":
		return path == "bytes" && recv == "Buffer"
	}
	return false
}

func viewDefs(pass *analysis.Pass, body ast.Node) map[*types.Var]ast.Expr {
	out := map[*types.Var]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			viewDefAdd(pass, out, as.Lhs[0], as.Rhs[0])
			return true
		}
		if len(as.Rhs) == 1 && len(as.Lhs) == 2 {
			viewDefAdd(pass, out, as.Lhs[0], as.Rhs[0])
		}
		return true
	})
	return out
}

func viewDefAdd(pass *analysis.Pass, out map[*types.Var]ast.Expr, lhs, rhs ast.Expr) {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return
	}
	v, ok := pass.TypesInfo.Defs[id].(*types.Var)
	if !ok {
		return
	}
	recv, ok := viewCallOf(pass, rhs)
	if !ok {
		return
	}
	out[v] = recv
}

func viewCallOf(pass *analysis.Pass, e ast.Expr) (ast.Expr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	return viewCallRecv(pass, call)
}

func viewCallRecv(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Bytes", "Peek", "ReadSlice":
	default:
		return nil, false
	}
	obj := typeutil.StaticCallee(pass.TypesInfo, call)
	if obj == nil {
		return nil, false
	}
	recv := obj.Signature().Recv()
	if recv == nil {
		return nil, false
	}
	if !isViewSourceType(recv.Type(), sel.Sel.Name) {
		return nil, false
	}
	return sel.X, true
}

func isViewSourceType(t types.Type, name string) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	path := obj.Pkg().Path()
	recv := obj.Name()
	switch name {
	case "Bytes":
		return (path == "bufio" && recv == "Scanner") || (path == "bytes" && recv == "Buffer")
	case "Peek", "ReadSlice":
		return path == "bufio" && recv == "Reader"
	}
	return false
}

func sinkExprs(pass *analysis.Pass, body ast.Node) []ast.Expr {
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
			out = appendAssignRhs(pass, x, out)
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

func appendAssignRhs(pass *analysis.Pass, as *ast.AssignStmt, out []ast.Expr) []ast.Expr {
	for i, lhs := range as.Lhs {
		if fieldOrMapTarget(lhs) {
			out = appendRhs(as, i, out)
			continue
		}
		if outsideVar(pass, lhs) {
			out = appendRhs(as, i, out)
		}
	}
	return out
}

func appendRhs(as *ast.AssignStmt, i int, out []ast.Expr) []ast.Expr {
	if len(as.Rhs) == 1 && len(as.Lhs) > 1 {
		return out
	}
	if len(as.Rhs) == len(as.Lhs) {
		return append(out, as.Rhs[i])
	}
	if len(as.Rhs) == 1 {
		return append(out, as.Rhs[0])
	}
	return out
}

func fieldOrMapTarget(lhs ast.Expr) bool {
	switch ast.Unparen(lhs).(type) {
	case *ast.SelectorExpr, *ast.IndexExpr:
		return true
	}
	return false
}

func outsideVar(pass *analysis.Pass, lhs ast.Expr) bool {
	id, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return false
	}
	_, isDef := pass.TypesInfo.Defs[id]
	return !isDef
}

func sinkView(pass *analysis.Pass, s ast.Expr, views map[*types.Var]ast.Expr) (ast.Expr, ast.Expr, bool) {
	inner := ast.Unparen(s)
	if recv, ok := viewCallOf(pass, inner); ok {
		return inner, recv, true
	}
	id, ok := ast.Unparen(s).(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok {
		return nil, nil, false
	}
	recv, ok := views[v]
	if !ok {
		return nil, nil, false
	}
	return id, recv, true
}
