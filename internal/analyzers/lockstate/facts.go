package lockstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/gosuda/gglint/internal/flow"
)

type fnFacts struct {
	info      *types.Info
	fn        ast.Node
	mentionAt map[types.Object][]token.Pos
	defines   map[types.Object]ast.Expr
	vars      map[types.Object]ast.Expr
	stored    map[types.Object][]flow.Key
	exposed   map[types.Object][]exposure
	funs      map[flow.Key]map[ast.Expr]bool
}

type exposure struct {
	key     flow.Key
	sel     *ast.SelectorExpr
	excl    flow.Key
	hasExcl bool
}

func collectFacts(info *types.Info, fn ast.Node) *fnFacts {
	f := &fnFacts{
		info:      info,
		fn:        fn,
		mentionAt: map[types.Object][]token.Pos{},
		defines:   map[types.Object]ast.Expr{},
		vars:      map[types.Object]ast.Expr{},
		stored:    map[types.Object][]flow.Key{},
		exposed:   map[types.Object][]exposure{},
		funs:      map[flow.Key]map[ast.Expr]bool{},
	}
	f.collectLockCalls()
	f.collectDeclValues()
	f.collectStoredAddresses()
	f.collectExposures()
	f.collectMentions()
	return f
}

func (f *fnFacts) collectLockCalls() {
	ast.Inspect(f.fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, _, ok := mutexKey(f.info, call)
		if !ok {
			return true
		}
		if f.funs[k] == nil {
			f.funs[k] = map[ast.Expr]bool{}
		}
		f.funs[k][ast.Unparen(call.Fun)] = true
		return true
	})
}

func (f *fnFacts) collectDeclValues() {
	fnLit := fnLitOf(f.fn)
	seen := map[types.Object]bool{}
	ast.Inspect(f.fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			f.collectDefine(n)
		case *ast.ValueSpec:
			f.collectVarValue(n, seen)
		}
		return true
	})
}

func (f *fnFacts) collectDefine(as *ast.AssignStmt) {
	if as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || id.Name == "_" {
		return
	}
	v := f.info.Defs[id]
	if v == nil {
		return
	}
	if _, ok := f.defines[v]; !ok {
		f.defines[v] = as.Rhs[0]
	}
}

func (f *fnFacts) collectVarValue(vs *ast.ValueSpec, seen map[types.Object]bool) {
	for i, name := range vs.Names {
		if name.Name == "_" {
			continue
		}
		v := f.info.Defs[name]
		if v == nil || seen[v] {
			continue
		}
		seen[v] = true
		if len(vs.Values) == 1 {
			f.vars[v] = vs.Values[0]
		} else if i < len(vs.Values) {
			f.vars[v] = vs.Values[i]
		}
	}
}

func (f *fnFacts) collectStoredAddresses() {
	fnLit := fnLitOf(f.fn)
	ast.Inspect(f.fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, rhs := range n.Rhs {
				f.markAddrKeys(rhs)
			}
		case *ast.ValueSpec:
			for _, value := range n.Values {
				f.markAddrKeys(value)
			}
		}
		return true
	})
}

func (f *fnFacts) markAddrKeys(e ast.Expr) {
	ast.Inspect(e, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		u, ok := x.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		if k, ok := flow.KeyOf(f.info, u.X); ok {
			f.stored[k.Root] = append(f.stored[k.Root], k)
		}
		return true
	})
}

func (f *fnFacts) collectExposures() {
	ast.Inspect(f.fn, func(n ast.Node) bool {
		f.exposeNode(n)
		return true
	})
}

func (f *fnFacts) exposeNode(n ast.Node) {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		f.exposeUnary(n)
	case *ast.SelectorExpr:
		f.markExposedSelector(n)
	case *ast.CallExpr:
		f.markExposedCall(n)
	case *ast.ReturnStmt:
		f.exposeResults(n.Results)
	case *ast.SendStmt:
		f.markExposed(n.Value, exposure{})
	case *ast.GoStmt:
		f.markExposedCallKeys(n.Call, exposure{})
	case *ast.FuncLit:
		f.exposeLiteral(n)
	case *ast.AssignStmt:
		f.exposeStored(n)
	}
}

func (f *fnFacts) exposeUnary(n *ast.UnaryExpr) {
	if n.Op == token.AND {
		f.markExposed(n.X, exposure{})
	}
}

func (f *fnFacts) exposeResults(results []ast.Expr) {
	for _, r := range results {
		f.markExposed(r, exposure{})
	}
}

func (f *fnFacts) exposeLiteral(lit *ast.FuncLit) {
	if lit != f.fn {
		f.markLitKeys(lit)
	}
}

func (f *fnFacts) exposeStored(as *ast.AssignStmt) {
	if !storedLHS(f.info, f.fn, as.Lhs) {
		return
	}
	for _, rhs := range as.Rhs {
		f.markExposed(rhs, exposure{})
	}
}

func (f *fnFacts) markExposed(e ast.Expr, base exposure) {
	k, ok := flow.KeyOf(f.info, e)
	if !ok {
		return
	}
	c := base
	c.key = k
	f.exposed[k.Root] = append(f.exposed[k.Root], c)
}

func (f *fnFacts) markExposedSelector(sel *ast.SelectorExpr) {
	s := f.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal || !isLockMethodName(s.Obj().Name()) {
		return
	}
	k, ok := flow.KeyOf(f.info, sel.X)
	if !ok {
		return
	}
	f.exposed[k.Root] = append(f.exposed[k.Root], exposure{key: k, sel: sel})
}

func (f *fnFacts) markExposedCall(call *ast.CallExpr) {
	k, _, isLock := mutexKey(f.info, call)
	if isLock {
		f.markExposedCallKeys(call, exposure{excl: k, hasExcl: true})
		return
	}
	f.markExposedCallKeys(call, exposure{})
}

func (f *fnFacts) markExposedCallKeys(call *ast.CallExpr, base exposure) {
	mark := func(e ast.Expr) {
		f.markExposed(e, base)
	}
	for _, arg := range call.Args {
		mark(arg)
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		mark(sel.X)
	}
}

func (f *fnFacts) markLitKeys(lit *ast.FuncLit) {
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		e, ok := x.(ast.Expr)
		if !ok {
			return true
		}
		k, ok := flow.KeyOf(f.info, e)
		if !ok {
			return true
		}
		f.exposed[k.Root] = append(f.exposed[k.Root], exposure{key: k})
		return false
	})
}

func (f *fnFacts) entryState(fn ast.Node, key flow.Key) state {
	v, ok := key.Root.(*types.Var)
	if !ok {
		return unknownState
	}
	if !flow.IsLocal(f.info, fn, v) {
		return unknownState
	}
	if _, ok := v.Type().Underlying().(*types.Pointer); ok {
		return unknownState
	}
	if pathHasPointer(v.Type(), key.Path) {
		return unknownState
	}
	if def, ok := f.declValue(v); ok && !emptyLiteral(def) {
		return unknownState
	}
	if f.lockExposed(key) {
		return unknownState
	}
	return freeState
}

func (f *fnFacts) declValue(v *types.Var) (ast.Expr, bool) {
	if e, ok := f.defines[v]; ok {
		return e, true
	}
	e, ok := f.vars[v]
	return e, ok
}

func (f *fnFacts) storedAddress(key flow.Key) bool {
	for _, k := range f.stored[key.Root] {
		if keyEnclosesKey(k, key) {
			return true
		}
	}
	return false
}

func (f *fnFacts) lockExposed(key flow.Key) bool {
	for _, c := range f.exposed[key.Root] {
		if !keyEnclosesKey(c.key, key) {
			continue
		}
		if c.hasExcl && c.excl == key {
			continue
		}
		if c.sel != nil && f.funs[key][c.sel] {
			continue
		}
		return true
	}
	return false
}

func (f *fnFacts) collectMentions() {
	ast.Inspect(f.fn, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := cmp.Or(f.info.Uses[id], f.info.Defs[id])
		if obj == nil {
			return true
		}
		f.mentionAt[obj] = append(f.mentionAt[obj], id.Pos())
		return true
	})
	for obj := range f.mentionAt {
		slices.Sort(f.mentionAt[obj])
	}
}

func (f *fnFacts) mentions(n ast.Node, root types.Object) bool {
	at := f.mentionAt[root]
	if len(at) == 0 {
		return false
	}
	pos := n.Pos()
	end := n.End()
	if !pos.IsValid() || !end.IsValid() {
		return false
	}
	i, _ := slices.BinarySearch(at, pos)
	return i < len(at) && at[i] < end
}

func reassignsRoot(info *types.Info, n ast.Node, key flow.Key) bool {
	v, ok := key.Root.(*types.Var)
	if !ok {
		return false
	}
	hit := false
	ast.Inspect(n, func(x ast.Node) bool {
		if hit {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if invalidatesKey(info, x, key, v) {
			hit = true
			return false
		}
		return true
	})
	return hit
}

func invalidatesKey(info *types.Info, x ast.Node, key flow.Key, v *types.Var) bool {
	as, ok := x.(*ast.AssignStmt)
	if !ok {
		return false
	}
	return assignInvalidates(info, as, key, v)
}

func assignInvalidates(info *types.Info, as *ast.AssignStmt, key flow.Key, v *types.Var) bool {
	for _, lhs := range as.Lhs {
		if targetInvalidates(info, lhs, key, v) {
			return true
		}
	}
	return false
}

func targetInvalidates(info *types.Info, e ast.Expr, key flow.Key, v *types.Var) bool {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok && id.Name != "_" && info.Uses[id] == v {
		return true
	}
	k, ok := flow.KeyOf(info, e)
	if !ok || k.Root != key.Root {
		return false
	}
	if k.Path == key.Path {
		return true
	}
	if k.Path == "" {
		return true
	}
	return prefixPath(key.Path, k.Path)
}

func addrInExpr(info *types.Info, e ast.Expr, key flow.Key) bool {
	handed := false
	ast.Inspect(e, func(x ast.Node) bool {
		if handed {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND && encloses(info, u.X, key) {
			handed = true
			return false
		}
		return true
	})
	return handed
}

func handsAddress(info *types.Info, n ast.Node, key flow.Key) bool {
	switch n := n.(type) {
	case *ast.DeferStmt:
		for _, arg := range n.Call.Args {
			if addrInExpr(info, arg, key) {
				return true
			}
		}
	case *ast.SendStmt:
		return addrInExpr(info, n.Value, key)
	case *ast.ReturnStmt:
		for _, r := range n.Results {
			if addrInExpr(info, r, key) {
				return true
			}
		}
	}
	return false
}
