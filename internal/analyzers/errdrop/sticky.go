package errdrop

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

type sticky struct{}

func (*sticky) AFact() {}

func computeSticky(pass *analysis.Pass) map[*types.Func]bool {
	out := map[*types.Func]bool{}
	decls, objs := stickyCandidates(pass)
	fields := map[*types.Func]string{}
	for i, fd := range decls {
		if f, ok := stickyStoredField(pass, fd, objs[i]); ok {
			fields[originOf(objs[i])] = f
		}
	}
	for i := range decls {
		origin := originOf(objs[i])
		f, ok := fields[origin]
		if !ok {
			continue
		}
		if hasFieldGetter(pass, decls, objs, origin, f) {
			out[origin] = true
		}
	}
	return out
}

func stickyCandidates(pass *analysis.Pass) ([]*ast.FuncDecl, []*types.Func) {
	decls := []*ast.FuncDecl{}
	objs := []*types.Func{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv == nil {
				continue
			}
			obj, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			decls = append(decls, fd)
			objs = append(objs, obj)
		}
	}
	return decls, objs
}

func stickyStoredField(pass *analysis.Pass, fd *ast.FuncDecl, obj *types.Func) (string, bool) {
	recv := stickyRecv(obj)
	if recv == nil || !stickyErrorResult(obj) {
		return "", false
	}
	return stickyReturnField(pass, fd, recv)
}

func stickyReturnField(pass *analysis.Pass, fd *ast.FuncDecl, recv *types.Var) (string, bool) {
	field := ""
	holds := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ret, isRet := n.(*ast.ReturnStmt)
		if !isRet {
			return true
		}
		if len(ret.Results) != 1 || !returnIsRecvField(pass.TypesInfo, ret.Results[0], recv, &field) {
			holds = false
			return false
		}
		return true
	})
	if !holds || field == "" {
		return "", false
	}
	return field, true
}

func stickyRecv(obj *types.Func) *types.Var {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || sig.Recv().Name() == "" {
		return nil
	}
	return sig.Recv()
}

func stickyErrorResult(obj *types.Func) bool {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Results() == nil || sig.Results().Len() != 1 {
		return false
	}
	return isError(sig.Results().At(0).Type())
}

func returnIsRecvField(info *types.Info, e ast.Expr, recv *types.Var, field *string) bool {
	if isNilIdent(info, e) {
		return true
	}
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok || info.Uses[id] != recv {
		return false
	}
	if *field == "" {
		*field = sel.Sel.Name
	}
	return *field == sel.Sel.Name
}

func hasFieldGetter(pass *analysis.Pass, decls []*ast.FuncDecl, objs []*types.Func, self *types.Func, field string) bool {
	base := recvBaseName(self)
	if base == nil {
		return false
	}
	for i, fd := range decls {
		if originOf(objs[i]) == self {
			continue
		}
		if recvBaseName(objs[i]) == nil || recvBaseName(objs[i]).Origin() != base.Origin() {
			continue
		}
		if methodReturnsField(pass, fd, objs[i], field) {
			return true
		}
	}
	return false
}

func recvBaseName(obj *types.Func) *types.Named {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	t := sig.Recv().Type()
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = ptr.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}
	return named
}

func methodReturnsField(pass *analysis.Pass, fd *ast.FuncDecl, obj *types.Func, field string) bool {
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv()
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		ret, isRet := n.(*ast.ReturnStmt)
		if !isRet {
			return true
		}
		if returnYieldsField(pass, ret, recv, field) {
			found = true
			return false
		}
		return true
	})
	return found
}

func returnYieldsField(pass *analysis.Pass, ret *ast.ReturnStmt, recv *types.Var, field string) bool {
	for _, res := range ret.Results {
		sel, ok := ast.Unparen(res).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != field {
			continue
		}
		id, ok := ast.Unparen(sel.X).(*ast.Ident)
		if ok && pass.TypesInfo.Uses[id] == recv {
			return true
		}
	}
	return false
}
