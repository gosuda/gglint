package flow

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func signaled(pass *analysis.Pass, body *ast.FuncLit) []Key {
	info := pass.TypesInfo
	var out []Key
	seen := map[Key]bool{}
	add := func(k Key, ok bool) {
		if ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	deferredCloses(info, body, add)
	lastClose(info, body, add)
	return out
}

func deferredCloses(info *types.Info, body *ast.FuncLit, add func(Key, bool)) {
	for _, s := range body.Body.List {
		d, ok := s.(*ast.DeferStmt)
		if !ok {
			continue
		}
		add(closeChan(info, d.Call))
		addChanArgs(info, d.Call, add)
		lit, ok := ast.Unparen(d.Call.Fun).(*ast.FuncLit)
		if !ok || lit.Body == nil {
			continue
		}
		deferredLitSignals(info, lit.Body, add)
	}
}

func addChanArgs(info *types.Info, call *ast.CallExpr, add func(Key, bool)) {
	if call == nil {
		return
	}
	for _, a := range call.Args {
		if !isChanArg(info, a) {
			continue
		}
		add(KeyOf(info, a))
	}
}

func isChanArg(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)
	if t == nil {
		return false
	}
	_, ok := types.Unalias(t).Underlying().(*types.Chan)
	return ok
}

func deferredLitSignals(info *types.Info, body *ast.BlockStmt, add func(Key, bool)) {
	called := calledLiterals(body)
	ast.Inspect(body, func(x ast.Node) bool {
		if _, ok := x.(*ast.GoStmt); ok {
			return false
		}
		if lit, ok := x.(*ast.FuncLit); ok {
			if _, ok := called[lit]; !ok {
				return false
			}
			return true
		}
		if call, ok := x.(*ast.CallExpr); ok {
			add(closeChan(info, call))
			return true
		}
		if st, ok := x.(*ast.SendStmt); ok {
			add(KeyOf(info, st.Chan))
			return true
		}
		return true
	})
}

func lastClose(info *types.Info, body *ast.FuncLit, add func(Key, bool)) {
	n := len(body.Body.List)
	if n == 0 {
		return
	}
	switch s := body.Body.List[n-1].(type) {
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(s.X).(*ast.CallExpr); ok {
			add(closeChan(info, call))
			addChanArgs(info, call, add)
		}
	case *ast.SendStmt:
		add(KeyOf(info, s.Chan))
	}
}

func closeChan(info *types.Info, call *ast.CallExpr) (Key, bool) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != closeFun || len(call.Args) != 1 {
		return Key{}, false
	}
	if b, ok := info.Uses[id].(*types.Builtin); !ok || b.Name() != closeFun {
		return Key{}, false
	}
	return KeyOf(info, call.Args[0])
}
