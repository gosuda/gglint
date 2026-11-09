package reentrantlock

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

type lockKey struct {
	root types.Object
	path string
}

func (k lockKey) String() string {
	return k.root.Name() + k.path
}

func (k lockKey) extend(path string) lockKey {
	if k.root == nil {
		return k
	}
	return lockKey{root: k.root, path: k.path + path}
}

type binding struct {
	recv map[types.Object]lockKey
	decl *ast.FuncDecl
}

func (b binding) bindKey(k lockKey) lockKey {
	if k.root == nil {
		return k
	}
	if outer, ok := b.recv[k.root]; ok {
		return outer.extend(k.path)
	}
	if b.decl != nil && declaredIn(k.root, b.decl) {
		return lockKey{}
	}
	return k
}

func declaredIn(obj types.Object, decl *ast.FuncDecl) bool {
	pos := obj.Pos()
	return pos.IsValid() && decl.Pos() <= pos && pos < decl.End()
}

func (c *checker) keyOf(e ast.Expr) lockKey {
	path := ""
	for e != nil {
		if id, ok := e.(*ast.Ident); ok {
			if v, ok := c.pass.TypesInfo.Uses[id].(*types.Var); ok {
				return lockKey{root: v, path: path}
			}
			return lockKey{}
		}
		var field string
		e, field = c.keyStep(e)
		if field != "" {
			path = "." + field + path
		}
	}
	return lockKey{}
}

func (c *checker) keyStep(e ast.Expr) (inner ast.Expr, field string) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return x.X, ""
	case *ast.StarExpr:
		return x.X, ""
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return x.X, ""
		}
	case *ast.SelectorExpr:
		if sel := c.pass.TypesInfo.Selections[x]; sel != nil && sel.Kind() == types.FieldVal {
			return x.X, x.Sel.Name
		}
	}
	return nil, ""
}

func (c *checker) recvKey(sel *ast.SelectorExpr, recv ast.Expr) lockKey {
	k := c.keyOf(recv)
	s := c.pass.TypesInfo.Selections[sel]
	if k.root == nil || s == nil {
		return k
	}
	t := s.Recv()
	index := s.Index()
	for _, i := range index[:len(index)-1] {
		st, ok := deref(t).Underlying().(*types.Struct)
		if !ok {
			return lockKey{}
		}
		field := st.Field(i)
		k.path += "." + field.Name()
		t = field.Type()
	}
	return k
}

func deref(t types.Type) types.Type {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func (c *checker) mutexKey(call *ast.CallExpr, method string) lockKey {
	sel, recv, _ := c.callOperands(call)
	if sel == nil || !isMutexMethod(typeutil.StaticCallee(c.pass.TypesInfo, call), method) {
		return lockKey{}
	}
	return c.recvKey(sel, recv)
}

func (c *checker) callOperands(call *ast.CallExpr) (sel *ast.SelectorExpr, recv ast.Expr, args []ast.Expr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, nil, call.Args
	}
	s := c.pass.TypesInfo.Selections[sel]
	if s != nil && s.Kind() == types.MethodExpr && len(call.Args) > 0 {
		return sel, call.Args[0], call.Args[1:]
	}
	return sel, sel.X, call.Args
}

func isMutexMethod(fn *types.Func, method string) bool {
	if fn == nil {
		return false
	}
	name := fn.FullName()
	return name == "(*sync.Mutex)."+method || name == "(*sync.RWMutex)."+method
}

func (c *checker) lockStmtKey(s ast.Stmt) lockKey {
	es, ok := s.(*ast.ExprStmt)
	if !ok {
		return lockKey{}
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return lockKey{}
	}
	return c.mutexKey(call, "Lock")
}

func (c *checker) substitute(k lockKey, call *ast.CallExpr, fn *types.Func) lockKey {
	outer, bound := c.argumentKey(k.root, call, fn.Signature())
	if bound {
		return outer.extend(k.path)
	}
	if declaredIn(k.root, c.decls[fn]) {
		return lockKey{}
	}
	return k
}

func (c *checker) argumentKey(root types.Object, call *ast.CallExpr, sig *types.Signature) (lockKey, bool) {
	sel, recv, args := c.callOperands(call)
	if r := sig.Recv(); r != nil && r == root {
		if sel == nil {
			return lockKey{}, true
		}
		return c.recvKey(sel, recv), true
	}
	for i := range sig.Params().Len() {
		p := sig.Params().At(i)
		if p != root {
			continue
		}
		if _, ptr := p.Type().Underlying().(*types.Pointer); i >= len(args) || !ptr {
			return lockKey{}, true
		}
		return c.keyOf(args[i]), true
	}
	return lockKey{}, false
}
