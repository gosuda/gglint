package flow

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/packages"
)

func mkDir(full, content string) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o600)
}

func loadPackageDir(t *testing.T, dir string) *packages.Package {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Context: t.Context(),
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
		Env:     append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}, ".")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("load: got %d packages", len(pkgs))
	}
	return pkgs[0]
}

func testFuncDeclAt(t *testing.T, decls []ast.Decl, i int) *ast.FuncDecl {
	t.Helper()
	fd, ok := decls[i].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl %d not func", i)
	}
	return fd
}

func testLocalVar(t *testing.T, info *types.Info, decl *ast.FuncDecl) *types.Var {
	t.Helper()
	as, ok := decl.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("stmt not assign")
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		t.Fatalf("lhs not ident")
	}
	v, ok := info.Defs[id].(*types.Var)
	if !ok {
		t.Fatalf("not var")
	}
	return v
}

const extraSpawnSource = `package p
import "sync"
func helper(wg *sync.WaitGroup) { wg.Done() }
func helper2()                  {}
func useInt(i int)              { _ = i }
type waiter struct{}
func (waiter) Wait()            {}
type S struct{ mu sync.Mutex }
func (s S) v() { s.mu.Lock() }
func mexpr(mu *sync.Mutex) { (*sync.Mutex).Lock(mu) }
func switches(c bool, v any, ch chan int, wg *sync.WaitGroup) {
	w2 := waiter{}
	w2.Wait()
	switch c {
	case true:
		wg.Go(func() {})
		helper2()
	default:
		close(ch)
	}
	switch v.(type) {
	case int:
		defer wg.Go(func() {})
	}
	select {
	case <-ch:
		<-ch
		wg.Go(func() {})
	default:
		defer wg.Go(func() {})
	}
	for range []int{1} {
		wg.Go(func() {})
	}
outer:
	for {
		_ = 1
		break outer
	}
}
func paramsub() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func(w *sync.WaitGroup) {
		println("x")
		useInt(1)
		helper(&wg)
		defer w.Done()
	}(&wg)
	wg.Wait()
	go func(w sync.WaitGroup) {
		w.Done()
	}(wg)
	go helper(&wg)
}
func seljoin() int {
	done := make(chan struct{})
	other := make(chan int)
	go func() {
		defer close(done)
	}()
	select {
	case <-done:
	}
	for range done {
	}
	select {
	case <-other:
		return 1
	default:
		return 2
	}
	var mu sync.Mutex
	var mu2 sync.Mutex
	mu.Lock()
	mu2.Lock()
	defer mu.Unlock()
	return 0
}
func lastsend(ch chan int, v int) {
	go func() {
		ch <- v
	}()
	<-ch
	defer helper2()
}
`

func extraPass(t *testing.T) (pass *analysis.Pass, decls map[string]*ast.FuncDecl) {
	t.Helper()
	pkg := loadPackage(t, extraSpawnSource)
	pass = ctrlPass(t, pkg)
	decls = map[string]*ast.FuncDecl{}
	for _, d := range pkg.Syntax[0].Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls[fd.Name.Name] = fd
		}
	}
	return pass, decls
}

func TestSpawnShapes(t *testing.T) {
	t.Parallel()
	pass, decls := extraPass(t)
	spawns := Spawns(pass, decls["switches"])
	if len(spawns) != 5 {
		t.Fatalf("switches: got %d spawns, want 5", len(spawns))
	}
	if spawns[0].InLoop {
		t.Fatalf("switches: case spawn in loop")
	}
	if !spawns[4].InLoop {
		t.Fatalf("switches: range spawn not in loop")
	}
	ps := Spawns(pass, decls["paramsub"])
	if len(ps) != 3 {
		t.Fatalf("paramsub: got %d spawns, want 3", len(ps))
	}
	if ps[0].Group.zero() || ps[0].Group != ps[2].Group {
		t.Fatalf("paramsub: groups differ")
	}
	if ps[1].Group.zero() || ps[1].Group == ps[0].Group {
		t.Fatalf("paramsub: value param group wrong")
	}
	fg := NewGraph(pass, decls["paramsub"])
	if fg == nil {
		t.Fatalf("paramsub: nil graph")
	}
	if !Joined(pass, fg, ps[0], decls["paramsub"].Body.List[3]) {
		t.Fatalf("paramsub: not joined at Wait")
	}
	if Spawns(nil, nil) != nil {
		t.Fatalf("nil pass spawns")
	}
	if Spawns(pass, &ast.BlockStmt{}) != nil {
		t.Fatalf("block spawns")
	}
	var lit *ast.FuncLit
	ast.Inspect(decls["switches"], func(n ast.Node) bool {
		if lit != nil {
			return false
		}
		if l, ok := n.(*ast.FuncLit); ok {
			lit = l
		}
		return true
	})
	if lit == nil {
		t.Fatalf("no literal found")
	}
	if len(Spawns(pass, lit)) != 0 {
		t.Fatalf("literal spawns")
	}
	if NewGraph(pass, lit) == nil {
		t.Fatalf("literal graph nil")
	}
}

func TestSelectJoins(t *testing.T) {
	t.Parallel()
	pass, decls := extraPass(t)
	sd := decls["seljoin"]
	spawns := Spawns(pass, sd)
	if len(spawns) != 1 {
		t.Fatalf("seljoin: got %d spawns", len(spawns))
	}
	if !spawns[0].Group.zero() {
		t.Fatalf("seljoin: group not zero")
	}
	g := NewGraph(pass, sd)
	if g == nil {
		t.Fatalf("seljoin: nil graph")
	}
	if !Joined(pass, g, spawns[0], sd.Body.List[1]) {
		t.Fatalf("seljoin: not joined at unreachable before-site node")
	}
	if !Joined(pass, g, spawns[0], sd.Body.List[5]) {
		t.Fatalf("seljoin: not joined after selects")
	}
	ld := decls["lastsend"]
	ls := Spawns(pass, ld)
	if len(ls) != 1 {
		t.Fatalf("lastsend: got %d spawns", len(ls))
	}
	lg := NewGraph(pass, ld)
	if !Joined(pass, lg, ls[0], ld.Body.List[1]) {
		t.Fatalf("lastsend: not joined at receive")
	}
	if Joined(pass, lg, ls[0], ls[0].Site) {
		t.Fatalf("lastsend: joined at site")
	}
	if Joined(pass, lg, ls[0], sd.Body.List[1]) {
		t.Fatalf("foreign node joined")
	}
	held := HeldAt(pass, g)
	if held(sd.Body.List[1]) != nil {
		t.Fatalf("seljoin: held before lock")
	}
}

func TestMethodExprKey(t *testing.T) {
	t.Parallel()
	pass, decls := extraPass(t)
	var mc *ast.CallExpr
	ast.Inspect(decls["mexpr"], func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && mc == nil {
			mc = c
		}
		return true
	})
	k, ok := MethodKey(pass.TypesInfo, mc)
	if !ok || k.Root.Name() != "mu" || k.Path != "" {
		t.Fatalf("method expr key: %v %v", k, ok)
	}
	vd := decls["v"]
	var vc *ast.CallExpr
	ast.Inspect(vd, func(n ast.Node) bool {
		c, isCall := n.(*ast.CallExpr)
		if isCall && vc == nil {
			vc = c
		}
		return true
	})
	k, ok = MethodKey(pass.TypesInfo, vc)
	if !ok || k.Root.Name() != "s" || k.Path != ".mu" {
		t.Fatalf("value receiver key: %v %v", k, ok)
	}
}

const extraEscapeSource = `package p
func conv(v int) any { return any(v) }
func imm(v int) int { return func(x int) int { return x + v }(1) }
func inc(v int) func() int { return func() int { v++; return v } }
func typelocal(v int) int {
	type local int
	var _ = local(v)
	return v
}
`

func TestEscapeExtra(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, extraEscapeSource)
	want := map[string]bool{"conv": false, "imm": false, "inc": true, "typelocal": true}
	for name, w := range want {
		decl := findDecl(f, name)
		if decl == nil {
			t.Fatalf("%s: decl missing", name)
		}
		var v *types.Var
		for _, field := range decl.Type.Params.List {
			for _, id := range field.Names {
				if id.Name == "v" {
					if obj, ok := info.Defs[id].(*types.Var); ok {
						v = obj
					}
				}
			}
		}
		if v == nil {
			t.Fatalf("%s: param missing", name)
		}
		if got := Escapes(info, decl, v); got != w {
			t.Fatalf("%s: Escapes = %v, want %v", name, got, w)
		}
	}
}

func TestFlowGuards(t *testing.T) {
	t.Parallel()
	if _, ok := KeyOf(nil, nil); ok {
		t.Fatalf("nil info key")
	}
	var g *Graph
	if g.Reaches(nil, nil, nil) {
		t.Fatalf("nil graph reaches")
	}
	Forward(g, 0, nil, nil, nil)
	if Joined(nil, nil, Spawn{}, nil) {
		t.Fatalf("nil join")
	}
	if HeldAt(nil, nil)(nil) != nil {
		t.Fatalf("nil held")
	}
	if Escapes(nil, nil, nil) {
		t.Fatalf("nil escapes")
	}
	if Spawns(nil, nil) != nil {
		t.Fatalf("nil spawns")
	}
	if StraightLine(nil, nil, nil) {
		t.Fatalf("nil straight")
	}
	if _, ok := DefinedBy(nil, nil, nil); ok {
		t.Fatalf("nil defined")
	}
	if IsLocal(nil, nil, nil) {
		t.Fatalf("nil local")
	}
	if NewGraph(nil, nil) != nil {
		t.Fatalf("nil graph")
	}
	if fnBody(&ast.BadStmt{}) != nil {
		t.Fatalf("bad body")
	}
	if ft, _ := funcSig(&ast.BadStmt{}); ft != nil {
		t.Fatalf("bad sig")
	}
	if !breaksLine(&ast.GoStmt{}) {
		t.Fatalf("go breaks")
	}
	if !breaksLine(&ast.IfStmt{}) {
		t.Fatalf("if breaks")
	}
	if !breaksLine(&ast.SwitchStmt{}) {
		t.Fatalf("switch breaks")
	}
	if !breaksLine(&ast.LabeledStmt{}) {
		t.Fatalf("labeled breaks")
	}
	if breaksLine(&ast.ExprStmt{}) {
		t.Fatalf("expr breaks")
	}
	f, _ := checkTypes(t, "package p\nfunc f() { go func() {}() }\n")
	decl := findDecl(f, "f")
	if fnBody(decl) == nil {
		t.Fatalf("decl body nil")
	}
	gs, ok := decl.Body.List[0].(*ast.GoStmt)
	if !ok {
		t.Fatalf("go stmt missing")
	}
	lit, ok := gs.Call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatalf("literal missing")
	}
	if fnBody(lit) == nil {
		t.Fatalf("literal body nil")
	}
	if ft, _ := funcSig(decl); ft == nil {
		t.Fatalf("decl sig nil")
	}
	if ft, _ := funcSig(lit); ft == nil {
		t.Fatalf("literal sig nil")
	}
}

func TestReachExtra(t *testing.T) {
	t.Parallel()
	f, _ := checkTypes(t, `package p
func f(c bool) int {
	x := 1
	var y, z, w int
	if c {
		y = 2
	} else {
		z = 3
	}
	w = 4
	return x + y + z + w
}
func g() int {
	a := 1
	b := 2
	return a + b
}
func h(c bool) int {
	if c {
	} else {
	}
	return 1
}
`)
	mayReturn := func(*ast.CallExpr) bool { return true }
	fdecl := findDecl(f, "f")
	fg := newGraph(fdecl, cfg.New(fdecl.Body, mayReturn))
	xa, ok := fdecl.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("first stmt not assign")
	}
	xid := xa.Lhs[0]
	wa, ok := fdecl.Body.List[3].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("fourth stmt not assign")
	}
	wid := wa.Lhs[0]
	if !fg.Reaches(xid, wid, nil) {
		t.Fatalf("subexpression queries missed")
	}
	gdecl := findDecl(f, "g")
	gg := newGraph(gdecl, cfg.New(gdecl.Body, mayReturn))
	if gg.Reaches(gdecl.Body.List[0], fdecl.Body.List[0], nil) {
		t.Fatalf("cross-graph reach")
	}
	if fg.Reaches(nil, wid, nil) {
		t.Fatalf("nil from reaches")
	}
	if fg.Reaches(xid, nil, nil) {
		t.Fatalf("nil to reaches")
	}
	hdecl := findDecl(f, "h")
	hg := newGraph(hdecl, cfg.New(hdecl.Body, mayReturn))
	if !hg.Reaches(hdecl.Body.List[0], hdecl.Body.List[1], nil) {
		t.Fatalf("empty branches block")
	}
	Forward(hg, 0, func(_ ast.Node, s int) int { return s }, func(a, _ int) int { return a }, nil)
}

func TestStraightLineExtra(t *testing.T) {
	t.Parallel()
	f, _ := checkTypes(t, `package p
func f(c bool) int {
	a := 1
	if c {
		p := 1
		q := 2
		_ = p + q
	}
	return a
}
`)
	decl := findDecl(f, "f")
	ifs, ok := decl.Body.List[1].(*ast.IfStmt)
	if !ok {
		t.Fatalf("second stmt not if")
	}
	ib := ifs.Body.List
	if !StraightLine(decl, ib[0], ib[1]) {
		t.Fatalf("later list missed")
	}
	if !StraightLine(decl, ib[1], ib[0]) {
		t.Fatalf("reversed missed")
	}
}

func TestHeldAtLoop(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nvar mu sync.Mutex\nfunc f(xs []int) {\nmu.Lock()\nfor range xs {\ng()\n}\nmu.Unlock()\n}\nfunc g() {}\n")
	pass := ctrlPass(t, pkg)
	fd, ok := pkg.Syntax[0].Decls[2].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl not func")
	}
	held := HeldAt(pass, NewGraph(pass, fd))
	rg, ok := fd.Body.List[1].(*ast.RangeStmt)
	if !ok {
		t.Fatalf("second stmt not range")
	}
	body := rg.Body.List[0]
	got := held(body)
	if len(got) != 1 || got[0].Root.Name() != "mu" {
		t.Fatalf("held in loop = %v, want [mu]", got)
	}
}

func TestForwardDeadBlock(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nvar mu sync.Mutex\nfunc f() {\nmu.Lock()\nreturn\nmu.Unlock()\n}\n")
	pass := ctrlPass(t, pkg)
	fd, ok := pkg.Syntax[0].Decls[2].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl not func")
	}
	name := func(n ast.Node) string {
		es, ok := n.(*ast.ExprStmt)
		if !ok {
			return ""
		}
		c, ok := es.X.(*ast.CallExpr)
		if !ok {
			return ""
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		return s.Sel.Name
	}
	var atUnlock []int
	Forward(NewGraph(pass, fd), 1,
		func(n ast.Node, s int) int {
			switch name(n) {
			case "Lock":
				return 1
			case "Unlock":
				return 0
			}
			return s
		},
		func(a, b int) int {
			if a == b {
				return a
			}
			return 0
		},
		func(n ast.Node, s int) {
			if name(n) == "Unlock" {
				atUnlock = append(atUnlock, s)
			}
		})
	if len(atUnlock) != 0 {
		t.Fatalf("dead unlock visited with %v, want no visit", atUnlock)
	}
}

func TestJoinedInLiteral(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nfunc f() int {\nrun := func() int {\ndone := make(chan struct{})\ngo func() { defer close(done) }()\n<-done\nreturn 1\n}\nreturn run()\n}")
	pass := ctrlPass(t, pkg)
	fd0 := testFuncDeclAt(t, pkg.Syntax[0].Decls, 0)
	as0, ok := fd0.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("first stmt not assign")
	}
	lit, ok := as0.Rhs[0].(*ast.FuncLit)
	if !ok {
		t.Fatalf("rhs not literal")
	}
	g := NewGraph(pass, lit)
	s := Spawns(pass, lit)[0]
	if !Joined(pass, g, s, lit.Body.List[3]) {
		t.Fatalf("not joined at receive in literal")
	}
}

func TestEscapesClose(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\nfunc f() {\nch := make(chan int)\nclose(ch)\n_ = 1\nclose(ch)\n}")
	decl, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl not func")
	}
	as, ok := decl.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("first stmt not assign")
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		t.Fatalf("lhs not ident")
	}
	ch, ok := info.Defs[id].(*types.Var)
	if !ok {
		t.Fatalf("ch not var")
	}
	if Escapes(info, decl, ch) {
		t.Fatalf("close escapes")
	}
}

func TestEscapesGoReceiver(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype T struct{ n int }\nfunc (t *T) run() {}\nfunc f() {\nt := &T{}\ngo t.run()\n}")
	decl := testFuncDeclAt(t, f.Decls, 2)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("go receiver does not escape")
	}
}

func TestEscapesMethodValue(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype T struct{ n int }\nfunc (t *T) run() {}\nvar registry []func()\nfunc f() {\nt := &T{}\ng := t.run\nregistry = append(registry, g)\n}")
	decl := testFuncDeclAt(t, f.Decls, 3)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("method value does not escape")
	}
}

func TestEscapesCapturedReceiver(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype R struct{ n int }\nfunc (r *R) Close() error { return nil }\nvar keep func() error\nfunc f() {\nr := &R{}\nkeep = func() error { return r.Close() }\n}")
	decl := testFuncDeclAt(t, f.Decls, 3)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("captured receiver does not escape")
	}
}

func TestEscapesDerefStore(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype T struct{ n int }\nfunc f(p **T) {\nt := &T{}\n*p = t\n}")
	decl := testFuncDeclAt(t, f.Decls, 1)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("deref store does not escape")
	}
}

func TestEscapesAlias(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype T struct{ n int }\nfunc f() *T {\nt := &T{}\np := t\nreturn p\n}")
	decl := testFuncDeclAt(t, f.Decls, 1)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("alias return does not escape")
	}
}

func TestEscapesDeleteArg(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\nfunc f(k string) int {\nm := map[string]int{\"a\": 1}\ndelete(m, k)\nreturn 0\n}")
	decl := testFuncDeclAt(t, f.Decls, 0)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("delete argument does not escape")
	}
}

func TestEscapeChainLinear(t *testing.T) {
	t.Parallel()
	chain := "b" + strings.Repeat(".Next()", 30)
	src := "package p\ntype B struct{}\nfunc (b *B) Next() *B { return b }\nfunc f() {\nb := &B{}\n_ = " + chain + "\n}"
	f, info := checkTypes(t, src)
	decl := testFuncDeclAt(t, f.Decls, 2)
	v := testLocalVar(t, info, decl)
	done := make(chan bool, 1)
	go func() { done <- Escapes(info, decl, v) }()
	select {
	case got := <-done:
		if got {
			t.Fatalf("chain receiver escapes")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("escape walk exceeded 10s: not linear")
	}
}

func TestEscapesDeferredLiteral(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\nimport \"os\"\nfunc f(p string) error {\nfh, err := os.Open(p)\nif err != nil {\nreturn err\n}\ndefer func() { fh.Close() }()\nreturn nil\n}")
	decl := testFuncDeclAt(t, f.Decls, 1)
	v := testLocalVar(t, info, decl)
	if Escapes(info, decl, v) {
		t.Fatalf("deferred literal captures fh")
	}
}

func TestEscapesInPlaceLiteral(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\nfunc f(v int) int {\nreturn func(x int) int { return x + v }(1)\n}")
	decl := testFuncDeclAt(t, f.Decls, 0)
	var v *types.Var
	for _, field := range decl.Type.Params.List {
		if obj, ok := info.Defs[field.Names[0]].(*types.Var); ok {
			v = obj
		}
	}
	if v == nil {
		t.Fatalf("param missing")
	}
	if Escapes(info, decl, v) {
		t.Fatalf("in-place literal captures v")
	}
}

func TestEscapesFieldRead(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype T struct{ n int }\nfunc f() int {\nt := &T{n: 1}\nn := t.n\nreturn n\n}")
	decl := testFuncDeclAt(t, f.Decls, 1)
	v := testLocalVar(t, info, decl)
	if Escapes(info, decl, v) {
		t.Fatalf("field read aliases t")
	}
}

func TestEscapesNamedResult(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\ntype T struct{ n int }\nfunc f() (r *T) {\nt := &T{}\nr = t\nreturn\n}")
	decl := testFuncDeclAt(t, f.Decls, 1)
	v := testLocalVar(t, info, decl)
	if !Escapes(info, decl, v) {
		t.Fatalf("named-result alias does not escape")
	}
}

func TestDefinedByRange(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\nfunc f(xs []int) int {\nv := 1\nfor v = range xs {\n}\nreturn v\n}")
	decl := testFuncDeclAt(t, f.Decls, 0)
	v := testLocalVar(t, info, decl)
	if _, ok := DefinedBy(info, decl, v); ok {
		t.Fatalf("range assignment still defined")
	}
}

func TestIsLocalTypeSwitch(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nfunc f(x any) int {\nswitch v := x.(type) {\ncase int:\nreturn v\n}\nreturn 0\n}")
	decl := testFuncDeclAt(t, pkg.Syntax[0].Decls, 0)
	info := pkg.TypesInfo
	var v *types.Var
	for _, obj := range info.Implicits {
		if vv, ok := obj.(*types.Var); ok && vv.Name() == "v" {
			v = vv
		}
	}
	if v == nil {
		t.Fatalf("type switch var missing")
	}
	if !IsLocal(info, decl, v) {
		t.Fatalf("type switch var not local")
	}
}

func TestKeyOfQualified(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"os\"\nfunc f() { _ = os.Args }")
	pass := ctrlPass(t, pkg)
	fdq := testFuncDeclAt(t, pkg.Syntax[0].Decls, 1)
	asq, ok := fdq.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("first stmt not assign")
	}
	rhs := asq.Rhs[0]
	k, ok := KeyOf(pass.TypesInfo, rhs)
	if !ok {
		t.Fatalf("qualified key failed: %v %v", k, ok)
	}
	if k.Root == nil || k.Root.Name() != "Args" || k.Root.Pkg() == nil || k.Root.Pkg().Path() != "os" {
		t.Fatalf("qualified root = %v, want os.Args", k.Root)
	}
	if k.Path != "" {
		t.Fatalf("qualified path = %q, want empty", k.Path)
	}
}

func TestPromotedLock(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\ntype Inner struct{ mu sync.Mutex }\ntype S struct{ *Inner }\nfunc f(s *S) int {\ns.mu.Lock()\ns.Inner.mu.Unlock()\nreturn 0\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 3)
	held := HeldAt(pass, NewGraph(pass, fd))
	if got := held(fd.Body.List[2]); len(got) != 0 {
		t.Fatalf("promoted held = %v, want []", got)
	}
}

func TestSpawnsTryGoCond(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		full := dir + "/" + name
		if err := mkDir(full, content); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeFile("go.mod", "module golang.org/x/sync\n\ngo 1.27\n")
	writeFile("p.go", "package p\nimport \"golang.org/x/sync/errgroup\"\nfunc f(g *errgroup.Group) bool {\nif !g.TryGo(func() error { return nil }) {\nreturn false\n}\nreturn true\n}\n")
	writeFile("errgroup/errgroup.go", "package errgroup\ntype Group struct{}\nfunc (g *Group) Go(f func() error) {}\nfunc (g *Group) TryGo(f func() error) bool { return true }\n")
	pkg := loadPackageDir(t, dir)
	pass := ctrlPass(t, pkg)
	fd := findDecl(pkg.Syntax[0], "f")
	if fd == nil {
		t.Fatalf("func f missing")
	}
	if len(Spawns(pass, fd)) != 1 {
		t.Fatalf("trygo in cond: got %d spawns, want 1", len(Spawns(pass, fd)))
	}
}

func TestGoGroupHelper(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nfunc helper(w *sync.WaitGroup) { w.Done() }\nfunc f() {\nvar wg sync.WaitGroup\nwg.Add(1)\ngo func(w *sync.WaitGroup) { helper(w) }(&wg)\nwg.Wait()\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 2)
	s := Spawns(pass, fd)[0]
	if s.Group.Root == nil || s.Group.Root.Name() != "wg" {
		t.Fatalf("group root = %v, want wg", s.Group.Root)
	}
}

func TestJoinedInPlaceWait(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nfunc f() int {\nvar wg sync.WaitGroup\nwg.Add(1)\ngo func() { defer wg.Done() }()\nfunc() { wg.Wait() }()\nreturn 0\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 1)
	if !Joined(pass, NewGraph(pass, fd), Spawns(pass, fd)[0], fd.Body.List[4]) {
		t.Fatalf("not joined at in-place wait")
	}
}

func TestJoinedUnreachable(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nfunc f() int {\ngo func() {}()\nreturn 0\nreturn 1\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 0)
	if !Joined(pass, NewGraph(pass, fd), Spawns(pass, fd)[0], fd.Body.List[2]) {
		t.Fatalf("not joined at unreachable")
	}
}

func TestGoGroupHelperPath(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\ntype S struct{ wg sync.WaitGroup }\nfunc f() {\nvar s S\ns.wg.Add(1)\ngo func(p *S) { defer p.wg.Done() }(&s)\ns.wg.Wait()\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 2)
	s := Spawns(pass, fd)[0]
	vd, ok := fd.Body.List[0].(*ast.DeclStmt)
	if !ok {
		t.Fatalf("first stmt not decl")
	}
	gen, ok := vd.Decl.(*ast.GenDecl)
	if !ok {
		t.Fatalf("decl not gen")
	}
	spec, ok := gen.Specs[0].(*ast.ValueSpec)
	if !ok {
		t.Fatalf("spec not value")
	}
	sobj := pass.TypesInfo.Defs[spec.Names[0]]
	if s.Group.Root != sobj || s.Group.Path != ".wg" {
		t.Fatalf("group key = (%v, %q), want (s, .wg)", s.Group.Root, s.Group.Path)
	}
	if !Joined(pass, NewGraph(pass, fd), s, fd.Body.List[3]) {
		t.Fatalf("not joined at wait")
	}
}

func TestSpawnPostNotInLoop(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nfunc f() {\nvar wg sync.WaitGroup\nfor i := 0; i < 2; wg.Go(func() {}) {\n}\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 1)
	sp := Spawns(pass, fd)
	if len(sp) != 1 {
		t.Fatalf("post spawn count = %d", len(sp))
	}
	if sp[0].InLoop {
		t.Fatalf("post-statement spawn marked in loop")
	}
}

func TestJoinedBeforeSite(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nfunc f() int {\nx := 1\ngo func() {}()\nreturn x\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 0)
	if !Joined(pass, NewGraph(pass, fd), Spawns(pass, fd)[0], fd.Body.List[0]) {
		t.Fatalf("not joined at unreachable before-site node")
	}
}

func TestJoinedInPlaceRecv(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nfunc f() int {\ndone := make(chan struct{})\ngo func() { defer close(done) }()\nfunc() { <-done }()\nreturn 0\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 0)
	g := NewGraph(pass, fd)
	s := Spawns(pass, fd)[0]
	if !Joined(pass, g, s, fd.Body.List[3]) {
		t.Fatalf("not joined at in-place receive")
	}
}

func TestHeldAfterTryLock(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nvar mu sync.Mutex\nfunc f() {\nmu.Lock()\n_ = mu.TryLock()\nmu.Unlock()\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 2)
	held := HeldAt(pass, NewGraph(pass, fd))
	got := held(fd.Body.List[2])
	if len(got) != 1 || got[0].Root.Name() != "mu" || got[0].Path != "" {
		t.Fatalf("held after TryLock = %v, want [mu]", got)
	}
}

func TestReachesNestedLiteral(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nfunc f(c chan int) {\ngo func() {\nc <- 1\n}()\n<-c\n}")
	pass := ctrlPass(t, pkg)
	fd := testFuncDeclAt(t, pkg.Syntax[0].Decls, 0)
	g := NewGraph(pass, fd)
	gs, ok := fd.Body.List[0].(*ast.GoStmt)
	if !ok {
		t.Fatalf("first stmt not go")
	}
	lit, ok := gs.Call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatalf("go fun not literal")
	}
	inner := lit.Body.List[0]
	if g.Reaches(inner, fd.Body.List[1], nil) {
		t.Fatalf("nested literal reaches")
	}
}
