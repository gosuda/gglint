package lockstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

type stateKind uint8

const (
	bottomKind stateKind = iota
	freeKind
	writeKind
	readKind
	unknownKind
)

type state struct {
	kind  stateKind
	depth int
	exact bool
}

var (
	bottomState  = state{kind: bottomKind}
	freeState    = state{kind: freeKind}
	writeState   = state{kind: writeKind}
	unknownState = state{kind: unknownKind}
)

func readState(depth int) state {
	return state{kind: readKind, depth: depth, exact: true}
}

func inexactReadState(depth int) state {
	return state{kind: readKind, depth: depth}
}

const (
	methodLock     = "Lock"
	methodRLock    = "RLock"
	methodUnlock   = "Unlock"
	methodRUnlock  = "RUnlock"
	methodTryLock  = "TryLock"
	methodTryRLock = "TryRLock"
)

func methodName(info *types.Info, call *ast.CallExpr) string {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return ""
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock":
		return methodLock
	case "(*sync.RWMutex).RLock":
		return methodRLock
	case "(*sync.Mutex).Unlock", "(*sync.RWMutex).Unlock":
		return methodUnlock
	case "(*sync.RWMutex).RUnlock":
		return methodRUnlock
	case "(*sync.Mutex).TryLock", "(*sync.RWMutex).TryLock":
		return methodTryLock
	case "(*sync.RWMutex).TryRLock":
		return methodTryRLock
	}
	return ""
}

func mutexKey(info *types.Info, call *ast.CallExpr) (flow.Key, string, bool) {
	m := methodName(info, call)
	if m == "" {
		return flow.Key{}, "", false
	}
	k, ok := flow.MethodKey(info, call)
	if !ok {
		return flow.Key{}, "", false
	}
	return k, m, true
}

func recvExpr(info *types.Info, call *ast.CallExpr) ast.Expr {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || info == nil {
		return nil
	}
	s := info.Selections[sel]
	if s == nil {
		return nil
	}
	if s.Kind() == types.MethodExpr {
		if len(call.Args) == 0 {
			return nil
		}
		return call.Args[0]
	}
	if s.Kind() != types.MethodVal {
		return nil
	}
	return sel.X
}

func recvText(info *types.Info, call *ast.CallExpr) string {
	e := recvExpr(info, call)
	if e == nil {
		return ""
	}
	return types.ExprString(e)
}

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
	switch n := x.(type) {
	case *ast.AssignStmt:
		return assignInvalidates(info, n, key, v)
	case *ast.IncDecStmt:
		return targetInvalidates(info, n.X, key, v)
	case *ast.RangeStmt:
		return rangeInvalidates(info, n, key, v)
	}
	return false
}

func assignInvalidates(info *types.Info, as *ast.AssignStmt, key flow.Key, v *types.Var) bool {
	for _, lhs := range as.Lhs {
		if targetInvalidates(info, lhs, key, v) {
			return true
		}
	}
	return false
}

func rangeInvalidates(info *types.Info, rs *ast.RangeStmt, key flow.Key, v *types.Var) bool {
	if rs.Key != nil && targetInvalidates(info, rs.Key, key, v) {
		return true
	}
	return rs.Value != nil && targetInvalidates(info, rs.Value, key, v)
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
	if key.Path == "" {
		return false
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

type helperCache struct {
	info  *types.Info
	pkg   *types.Package
	decls map[*types.Func]ast.Node
	memo  map[*types.Func]bool
}

func newHelperCache(pass *analysis.Pass) *helperCache {
	c := &helperCache{
		info:  pass.TypesInfo,
		pkg:   pass.Pkg,
		decls: map[*types.Func]ast.Node{},
		memo:  map[*types.Func]bool{},
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				return true
			}
			if fn, ok := c.info.Defs[fd.Name].(*types.Func); ok {
				if _, exists := c.decls[fn]; !exists {
					c.decls[fn] = fd.Body
				}
			}
			return true
		})
	}
	return c
}

func (c *helperCache) unlocks(fn *types.Func) bool {
	if v, ok := c.memo[fn]; ok {
		return v
	}
	body, ok := c.bodyOf(fn)
	if !ok {
		c.memo[fn] = false
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if m := methodName(c.info, call); m == methodUnlock || m == methodRUnlock {
			found = true
			return false
		}
		return true
	})
	c.memo[fn] = found
	return found
}

func (c *helperCache) bodyOf(fn *types.Func) (ast.Node, bool) {
	if body, ok := c.decls[fn]; ok {
		return body, true
	}
	if o := fn.Origin(); o != fn {
		body, ok := c.decls[o]
		return body, ok
	}
	return nil, false
}

func (c *helperCache) staticLocal(call *ast.CallExpr) (*types.Func, bool) {
	fn := typeutil.StaticCallee(c.info, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != c.pkg {
		return nil, false
	}
	if _, ok := c.decls[fn]; ok {
		return fn, true
	}
	if o := fn.Origin(); o != fn {
		if _, ok := c.decls[o]; ok {
			return o, true
		}
	}
	return nil, false
}

func (c *helperCache) passesRoot(call *ast.CallExpr, key flow.Key) bool {
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if k, ok := flow.KeyOf(c.info, sel.X); ok && k.Root == key.Root {
			return true
		}
	}
	for _, a := range call.Args {
		if k, ok := flow.KeyOf(c.info, a); ok && k.Root == key.Root {
			return true
		}
	}
	return false
}

func (c *helperCache) isPure(call *ast.CallExpr, key flow.Key) bool {
	fn, ok := c.staticLocal(call)
	if !ok || !c.passesRoot(call, key) {
		return false
	}
	return !c.unlocks(fn)
}

func (c *helperCache) isUnlocking(call *ast.CallExpr, key flow.Key) bool {
	fn, ok := c.staticLocal(call)
	if !ok || !c.passesRoot(call, key) {
		return false
	}
	return c.unlocks(fn)
}

func (c *helperCache) hasDeferredRelease(fn ast.Node, key flow.Key) bool {
	found := false
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		ds, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if c.deferReleases(ds, key) {
			found = true
			return false
		}
		return true
	})
	return found
}

func (c *helperCache) deferReleases(ds *ast.DeferStmt, key flow.Key) bool {
	if c.isUnlocking(ds.Call, key) {
		return true
	}
	lit, ok := ast.Unparen(ds.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return false
	}
	return c.litUnlocks(lit, key)
}

func (c *helperCache) litUnlocks(lit *ast.FuncLit, key flow.Key) bool {
	found := false
	ast.Inspect(lit.Body, func(x ast.Node) bool {
		if found {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if c.isUnlocking(call, key) {
			found = true
			return false
		}
		return true
	})
	return found
}

func emptyLiteral(e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	return ok && len(lit.Elts) == 0
}

func exposesMethodValue(info *types.Info, sel *ast.SelectorExpr, key flow.Key, funs map[ast.Expr]bool) bool {
	if funs[sel] {
		return false
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal || !isLockMethodName(s.Obj().Name()) {
		return false
	}
	return encloses(info, sel.X, key)
}

func isLockMethodName(m string) bool {
	switch m {
	case methodLock, methodRLock, methodUnlock, methodRUnlock, methodTryLock, methodTryRLock:
		return true
	}
	return false
}

func storedLHS(info *types.Info, fn ast.Node, lhs []ast.Expr) bool {
	for _, e := range lhs {
		if storedTarget(info, fn, e) {
			return true
		}
	}
	return false
}

func storedTarget(info *types.Info, fn ast.Node, lhs ast.Expr) bool {
	switch l := ast.Unparen(lhs).(type) {
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.StarExpr:
		return true
	case *ast.Ident:
		v, ok := info.Defs[l].(*types.Var)
		return ok && (v.Pos() < fn.Pos() || v.Pos() >= fn.End())
	}
	return false
}

func mutexFunSet(info *types.Info, fn ast.Node, key flow.Key) map[ast.Expr]bool {
	funs := map[ast.Expr]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if k, _, ok := mutexKey(info, call); ok && k == key {
				funs[ast.Unparen(call.Fun)] = true
			}
		}
		return true
	})
	return funs
}

func collectKeys(info *types.Info, fn ast.Node) []flow.Key {
	var fnLit *ast.FuncLit
	if l, ok := fn.(*ast.FuncLit); ok {
		fnLit = l
	}
	seen := map[flow.Key]bool{}
	var out []flow.Key
	ast.Inspect(fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok && l != fnLit {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, _, ok := mutexKey(info, call)
		if !ok || seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return out
}

func joinState(a, b state) state {
	if a.kind == bottomKind {
		return b
	}
	if b.kind == bottomKind {
		return a
	}
	if a == b {
		return a
	}
	if a.kind == readKind && b.kind == readKind && (!a.exact || !b.exact) {
		return inexactReadState(min(a.depth, b.depth))
	}
	return unknownState
}

func stepMethod(m string, cur state) state {
	switch m {
	case methodLock:
		return writeState
	case methodRLock:
		return stepRLock(cur)
	case methodUnlock:
		return stepUnlock(cur)
	case methodRUnlock:
		return stepRUnlock(cur)
	}
	return unknownState
}

func stepRLock(cur state) state {
	if cur.kind == writeKind {
		return unknownState
	}
	if cur.kind == readKind {
		if !cur.exact {
			return inexactReadState(cur.depth + 1)
		}
		return readState(cur.depth + 1)
	}
	if cur.kind == unknownKind {
		return inexactReadState(1)
	}
	return readState(1)
}

func stepUnlock(cur state) state {
	if cur.kind == writeKind {
		return freeState
	}
	return unknownState
}

func stepRUnlock(cur state) state {
	if cur.kind == readKind && !cur.exact {
		if cur.depth >= 2 {
			return inexactReadState(cur.depth - 1)
		}
		return unknownState
	}
	if cur.kind == readKind && cur.depth > 1 {
		return readState(cur.depth - 1)
	}
	if cur.kind == readKind {
		return freeState
	}
	return unknownState
}

func takesKey(info *types.Info, call *ast.CallExpr, key flow.Key) bool {
	for _, arg := range call.Args {
		if encloses(info, arg, key) {
			return true
		}
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if encloses(info, sel.X, key) {
			return true
		}
	}
	return false
}

func encloses(info *types.Info, e ast.Expr, key flow.Key) bool {
	k, ok := flow.KeyOf(info, e)
	if !ok {
		return false
	}
	return keyEnclosesKey(k, key)
}

func keyEnclosesKey(k, key flow.Key) bool {
	if k.Root != key.Root {
		return false
	}
	if k == key {
		return true
	}
	if k.Path == "" || key.Path == "" {
		return true
	}
	return prefixPath(key.Path, k.Path) || prefixPath(k.Path, key.Path)
}

func prefixPath(p, q string) bool {
	if len(p) <= len(q) || p[:len(q)] != q {
		return false
	}
	return p[len(q)] == '.'
}

func mentionsKey(info *types.Info, lit *ast.FuncLit, key flow.Key) bool {
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if found {
			return false
		}
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		k, ok := flow.KeyOf(info, e)
		if !ok {
			return true
		}
		if k.Root != key.Root {
			return false
		}
		if encloses(info, e, key) {
			found = true
		}
		return false
	})
	return found
}

func applyNode(info *types.Info, key flow.Key, n ast.Node, cur state, hc *helperCache) state {
	if cur == bottomState {
		return bottomState
	}
	if reassignsRoot(info, n, key) {
		return unknownState
	}
	if handsAddress(info, n, key) {
		return unknownState
	}
	if ds, ok := n.(*ast.DeferStmt); ok {
		return deferArgs(info, key, ds, cur, hc)
	}
	skips := mutexFunSet(info, n, key)
	next := cur
	ast.Inspect(n, func(x ast.Node) bool {
		var cont bool
		next, cont = visitNode(info, key, x, next, skips, hc)
		return cont
	})
	return next
}

func deferArgs(info *types.Info, key flow.Key, ds *ast.DeferStmt, cur state, hc *helperCache) state {
	for _, arg := range ds.Call.Args {
		if addrInExpr(info, arg, key) {
			return unknownState
		}
	}
	next := cur
	for _, arg := range ds.Call.Args {
		ast.Inspect(arg, func(x ast.Node) bool {
			var cont bool
			next, cont = visitNode(info, key, x, next, nil, hc)
			return cont
		})
	}
	return next
}

func visitNode(info *types.Info, key flow.Key, x ast.Node, next state, skips map[ast.Expr]bool, hc *helperCache) (state, bool) {
	if gs, ok := x.(*ast.GoStmt); ok {
		return visitGoStmt(info, gs, key, next)
	}
	if lit, ok := x.(*ast.FuncLit); ok {
		return visitLit(info, lit, key, next)
	}
	if sel, ok := x.(*ast.SelectorExpr); ok {
		if exposesMethodValue(info, sel, key, skips) {
			return unknownState, false
		}
		return next, true
	}
	return visitCall(info, key, x, next, hc)
}

func visitGoStmt(info *types.Info, gs *ast.GoStmt, key flow.Key, next state) (state, bool) {
	if goSharesKey(info, gs, key) {
		return unknownState, false
	}
	return next, false
}

func visitLit(info *types.Info, lit *ast.FuncLit, key flow.Key, next state) (state, bool) {
	if mentionsKey(info, lit, key) {
		return unknownState, false
	}
	return next, false
}

func visitCall(info *types.Info, key flow.Key, x ast.Node, next state, hc *helperCache) (state, bool) {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return next, true
	}
	if k, m, matched := mutexKey(info, call); matched && k == key {
		return stepMethod(m, next), true
	}
	if hc != nil && hc.isPure(call, key) {
		return next, true
	}
	if takesKey(info, call, key) {
		return unknownState, true
	}
	return next, true
}

func goSharesKey(info *types.Info, gs *ast.GoStmt, key flow.Key) bool {
	shared := false
	ast.Inspect(gs.Call, func(n ast.Node) bool {
		if shared {
			return false
		}
		if goNodeSharesKey(info, n, key) {
			shared = true
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		return true
	})
	return shared
}

func goNodeSharesKey(info *types.Info, n ast.Node, key flow.Key) bool {
	if lit, ok := n.(*ast.FuncLit); ok {
		return mentionsKey(info, lit, key)
	}
	if call, ok := n.(*ast.CallExpr); ok {
		return takesKey(info, call, key)
	}
	if u, ok := n.(*ast.UnaryExpr); ok {
		return encloses(info, u.X, key)
	}
	return false
}

func collectPre(g *flow.Graph, entry state, tr func(ast.Node, state) state) (map[ast.Node]state, []ast.Node) {
	out := map[ast.Node]state{}
	var order []ast.Node
	flow.Forward(g, entry, tr, joinState, func(n ast.Node, s state) {
		if _, ok := out[n]; !ok {
			order = append(order, n)
		}
		out[n] = s
	})
	return out, order
}

type opCall struct {
	call   *ast.CallExpr
	method string
	recv   string
}

func opsInNode(info *types.Info, n ast.Node, key flow.Key) []opCall {
	var out []opCall
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.GoStmt); ok {
			return false
		}
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, m, ok := mutexKey(info, call)
		if !ok || k != key {
			return true
		}
		out = append(out, opCall{call: call, method: m, recv: recvText(info, call)})
		return true
	})
	return out
}
