package flow

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/cfg"
)

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
	if Joined(pass, g, spawns[0], sd.Body.List[1]) {
		t.Fatalf("seljoin: joined before select")
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
	want := map[string]bool{"conv": false, "imm": true, "inc": true, "typelocal": true}
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
