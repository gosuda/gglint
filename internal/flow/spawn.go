package flow

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

type Spawn struct {
	Site   ast.Node     // *ast.GoStmt, or the Go/TryGo call expression
	Body   *ast.FuncLit // nil when the spawned function is not a literal
	InLoop bool         // Site is inside a for or range body of the enclosing function
	Group  Key          // WaitGroup or errgroup the spawn belongs to; zero when none
}

// Spawns returns the goroutine creation sites of fn in source order.
func Spawns(pass *analysis.Pass, fn ast.Node) []Spawn {
	body := Body(fn)
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return nil
	}
	w := spawnWalker{info: pass.TypesInfo, files: pass.Files}
	for _, s := range body.List {
		w.stmt(s, false)
	}
	return w.out
}

type spawnWalker struct {
	info  *types.Info
	files []*ast.File
	out   []Spawn
}

func (w *spawnWalker) stmt(s ast.Stmt, inLoop bool) {
	switch s := s.(type) {
	case *ast.GoStmt:
		w.addGo(s, inLoop)
	case *ast.BlockStmt:
		w.list(s.List, inLoop)
	case *ast.IfStmt:
		w.optStmt(s.Init, inLoop)
		w.scan(s.Cond, inLoop)
		w.stmt(s.Body, inLoop)
		w.optStmt(s.Else, inLoop)
	case *ast.ForStmt:
		w.optStmt(s.Init, inLoop)
		w.scan(s.Cond, inLoop)
		w.optStmt(s.Post, inLoop)
		w.stmt(s.Body, true)
	case *ast.RangeStmt:
		w.scan(s.X, inLoop)
		w.stmt(s.Body, true)
	case *ast.LabeledStmt:
		w.stmt(s.Stmt, inLoop)
	case *ast.SelectStmt:
		w.stmt(s.Body, inLoop)
	default:
		w.rest(s, inLoop)
	}
}

func (w *spawnWalker) list(l []ast.Stmt, inLoop bool) {
	for _, s := range l {
		w.stmt(s, inLoop)
	}
}

func (w *spawnWalker) rest(s ast.Stmt, inLoop bool) {
	if w.restClause(s, inLoop) {
		return
	}
	w.restExpr(s, inLoop)
}

func (w *spawnWalker) restClause(s ast.Stmt, inLoop bool) bool {
	switch s := s.(type) {
	case *ast.SwitchStmt:
		w.optStmt(s.Init, inLoop)
		w.scan(s.Tag, inLoop)
		w.stmt(s.Body, inLoop)
	case *ast.TypeSwitchStmt:
		w.optStmt(s.Init, inLoop)
		w.stmt(s.Assign, inLoop)
		w.stmt(s.Body, inLoop)
	case *ast.CaseClause:
		w.scanList(s.List, inLoop)
		for _, c := range s.Body {
			w.stmt(c, inLoop)
		}
	case *ast.CommClause:
		w.optStmt(s.Comm, inLoop)
		for _, c := range s.Body {
			w.stmt(c, inLoop)
		}
	default:
		return false
	}
	return true
}

func (w *spawnWalker) restExpr(s ast.Stmt, inLoop bool) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		w.scan(s.X, inLoop)
	case *ast.DeferStmt:
		w.scan(s.Call, inLoop)
	case *ast.AssignStmt:
		w.scanList(s.Lhs, inLoop)
		w.scanList(s.Rhs, inLoop)
	case *ast.ReturnStmt:
		w.scanList(s.Results, inLoop)
	case *ast.SendStmt:
		w.scan(s.Chan, inLoop)
		w.scan(s.Value, inLoop)
	case *ast.DeclStmt:
		w.scanDecl(s, inLoop)
	case *ast.IncDecStmt:
		w.scan(s.X, inLoop)
	}
}

func (w *spawnWalker) optStmt(s ast.Stmt, inLoop bool) {
	if s != nil {
		w.stmt(s, inLoop)
	}
}

func (w *spawnWalker) scan(e ast.Expr, inLoop bool) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			w.check(call, inLoop)
		}
		return true
	})
}

func (w *spawnWalker) scanList(l []ast.Expr, inLoop bool) {
	for _, e := range l {
		w.scan(e, inLoop)
	}
}

func (w *spawnWalker) scanDecl(s *ast.DeclStmt, inLoop bool) {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			w.scanList(vs.Values, inLoop)
		}
	}
}

func (w *spawnWalker) check(call *ast.CallExpr, inLoop bool) {
	callee := typeutil.StaticCallee(w.info, call)
	if callee == nil {
		return
	}
	var key Key
	switch callee.FullName() {
	case "(*sync.WaitGroup).Go", "(*golang.org/x/sync/errgroup.Group).Go", "(*golang.org/x/sync/errgroup.Group).TryGo":
		key, _ = MethodKey(w.info, call)
	default:
		return
	}
	var body *ast.FuncLit
	if len(call.Args) > 0 {
		if lit, ok := ast.Unparen(call.Args[0]).(*ast.FuncLit); ok {
			body = lit
		}
	}
	w.out = append(w.out, Spawn{Site: call, Body: body, InLoop: inLoop, Group: key})
}

func (w *spawnWalker) addGo(s *ast.GoStmt, inLoop bool) {
	var body *ast.FuncLit
	if lit, ok := ast.Unparen(s.Call.Fun).(*ast.FuncLit); ok {
		body = lit
	}
	w.out = append(w.out, Spawn{Site: s, Body: body, InLoop: inLoop, Group: w.goGroup(s, body)})
	w.scanList(s.Call.Args, inLoop)
}

func (w *spawnWalker) goGroup(s *ast.GoStmt, body *ast.FuncLit) Key {
	seen := map[Key]bool{}
	var cands []Key
	add := w.groupAdder(body, seen, &cands)
	if body == nil {
		w.groupFromArgs(s, add)
	} else {
		w.groupFromBody(s, body, add)
	}
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) != 0 {
		return Key{}
	}
	if body == nil || body.Body == nil {
		return Key{}
	}
	return w.helperGroup(body)
}

func (w *spawnWalker) helperGroup(body *ast.FuncLit) Key {
	seen := map[Key]bool{}
	var cands []Key
	add := func(k Key, ok bool) {
		if !ok || seen[k] {
			return
		}
		seen[k] = true
		cands = append(cands, k)
	}
	w.collectHelperGroups(body, add)
	if len(cands) == 1 {
		return cands[0]
	}
	return Key{}
}

func (w *spawnWalker) collectHelperGroups(body *ast.FuncLit, add func(Key, bool)) {
	ast.Inspect(body.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			w.helperCallDone(call, add)
		}
		return true
	})
}

func (w *spawnWalker) helperCallDone(call *ast.CallExpr, add func(Key, bool)) {
	fn := typeutil.StaticCallee(w.info, call)
	if fn == nil {
		return
	}
	decl := w.findDecl(fn)
	if decl == nil || decl.Body == nil {
		return
	}
	w.packageDoneKeys(decl.Body, add)
}

func (w *spawnWalker) findDecl(fn *types.Func) *ast.FuncDecl {
	for _, f := range w.files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && w.info.Defs[fd.Name] == fn {
				return fd
			}
		}
	}
	return nil
}

func (w *spawnWalker) packageDoneKeys(body *ast.BlockStmt, add func(Key, bool)) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		w.packageDoneCall(call, add)
		return true
	})
}

func (w *spawnWalker) packageDoneCall(call *ast.CallExpr, add func(Key, bool)) {
	fn := typeutil.StaticCallee(w.info, call)
	if fn == nil || fn.FullName() != "(*sync.WaitGroup).Done" {
		return
	}
	k, ok := MethodKey(w.info, call)
	if !ok || !w.isPackageVar(k.Root) {
		return
	}
	add(k, true)
}

func (w *spawnWalker) isPackageVar(v types.Object) bool {
	vv, ok := v.(*types.Var)
	if !ok || vv.Pkg() == nil {
		return false
	}
	return vv.Parent() == vv.Pkg().Scope()
}

func (w *spawnWalker) groupAdder(body *ast.FuncLit, seen map[Key]bool, cands *[]Key) func(Key, bool) {
	return func(k Key, ok bool) {
		if !ok || seen[k] {
			return
		}
		if body != nil && w.insideLiteral(body, k) {
			return
		}
		seen[k] = true
		*cands = append(*cands, k)
	}
}

func (w *spawnWalker) groupFromArgs(s *ast.GoStmt, add func(Key, bool)) {
	for _, a := range s.Call.Args {
		if isWaitGroup(w.info, a) {
			add(KeyOf(w.info, a))
		}
	}
}

func (w *spawnWalker) groupFromBody(s *ast.GoStmt, body *ast.FuncLit, add func(Key, bool)) {
	deferred := deferredDoneLits(body.Body)
	called := inplaceDoneLits(body.Body)
	ast.Inspect(body.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.GoStmt:
			return false
		case *ast.SelectorExpr:
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			if deferred[lit] || called[lit] {
				return true
			}
			return false
		}
		return w.literalCall(s, body, n, add)
	})
}

func deferredDoneLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if d, ok := n.(*ast.DeferStmt); ok {
			if lit, ok := ast.Unparen(d.Call.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
			return true
		}
		return true
	})
	return out
}

func inplaceDoneLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok {
			return false
		}
		if _, ok := n.(*ast.DeferStmt); ok {
			return true
		}
		if c, ok := n.(*ast.CallExpr); ok {
			if lit, ok := ast.Unparen(c.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
		}
		return true
	})
	return out
}

func (w *spawnWalker) insideLiteral(body *ast.FuncLit, k Key) bool {
	if k.Root == nil || body.Body == nil {
		return false
	}
	pos := k.Root.Pos()
	return pos >= body.Body.Pos() && pos < body.Body.End()
}

func (w *spawnWalker) literalCall(s *ast.GoStmt, body *ast.FuncLit, n ast.Node, add func(Key, bool)) bool {
	if id, ok := n.(*ast.Ident); ok {
		w.literalIdent(s, body, id, add)
		return true
	}
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return true
	}
	return w.literalCallExpr(s, body, call, add)
}

func (w *spawnWalker) literalIdent(s *ast.GoStmt, body *ast.FuncLit, id *ast.Ident, add func(Key, bool)) {
	if !isWaitGroup(w.info, id) {
		return
	}
	k, ok := KeyOf(w.info, id)
	add(w.subst(body, s.Call, k, ok))
}

func (w *spawnWalker) literalCallExpr(s *ast.GoStmt, body *ast.FuncLit, call *ast.CallExpr, add func(Key, bool)) bool {
	callee := typeutil.StaticCallee(w.info, call)
	if callee == nil {
		return true
	}
	if callee.FullName() == "(*sync.WaitGroup).Done" {
		k, ok := MethodKey(w.info, call)
		add(w.subst(body, s.Call, k, ok))
		return true
	}
	for _, a := range call.Args {
		if isWaitGroup(w.info, a) {
			k, ok := KeyOf(w.info, a)
			add(w.subst(body, s.Call, k, ok))
		}
	}
	return true
}

func (w *spawnWalker) subst(body *ast.FuncLit, call *ast.CallExpr, k Key, ok bool) (Key, bool) {
	if !ok {
		return Key{}, false
	}
	sig, isSig := w.info.TypeOf(body).(*types.Signature)
	if !isSig || sig == nil {
		return k, true
	}
	for i := range sig.Params().Len() {
		if sig.Params().At(i) != k.Root {
			continue
		}
		param := sig.Params().At(i)
		if _, isPtr := param.Type().Underlying().(*types.Pointer); !isPtr || i >= len(call.Args) {
			return k, true
		}
		argKey, argOK := KeyOf(w.info, call.Args[i])
		if !argOK {
			return Key{}, false
		}
		return Key{Root: argKey.Root, Path: argKey.Path + k.Path}, true
	}
	return k, true
}

func isWaitGroup(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	} else if p, ok := t.Underlying().(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	return n.Obj().Pkg().Path() == "sync" && n.Obj().Name() == "WaitGroup"
}
