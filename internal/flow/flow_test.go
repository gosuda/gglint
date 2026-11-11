package flow

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

func checkTypes(t *testing.T, src string) (*ast.File, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Types:      map[ast.Expr]types.TypeAndValue{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	conf := types.Config{Importer: importer.Default(), FakeImportC: true}
	if _, err := conf.Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	return f, info
}

func findDecl(f *ast.File, name string) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

func loadPackage(t *testing.T, src string) *packages.Package {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"go.mod": "module flowtest\n\ngo 1.27\n", "p.go": src}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	pkgs, err := packages.Load(&packages.Config{
		Context: t.Context(),
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule,
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

func ctrlPass(t *testing.T, pkg *packages.Package) *analysis.Pass {
	t.Helper()
	ires, err := inspect.Analyzer.Run(&analysis.Pass{Files: pkg.Syntax})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	cres, err := ctrlflow.Analyzer.Run(&analysis.Pass{
		Files:            pkg.Syntax,
		Fset:             pkg.Fset,
		TypesInfo:        pkg.TypesInfo,
		TypesSizes:       pkg.TypesSizes,
		ResultOf:         map[*analysis.Analyzer]any{inspect.Analyzer: ires},
		ImportObjectFact: func(types.Object, analysis.Fact) bool { return false },
		ExportObjectFact: func(types.Object, analysis.Fact) {},
	})
	if err != nil {
		t.Fatalf("ctrlflow: %v", err)
	}
	return &analysis.Pass{
		Files:      pkg.Syntax,
		Fset:       pkg.Fset,
		TypesInfo:  pkg.TypesInfo,
		TypesSizes: pkg.TypesSizes,
		ResultOf:   map[*analysis.Analyzer]any{ctrlflow.Analyzer: cres},
	}
}

func TestKeyOf(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, `package p
type T struct {
	F int
	G int
}
var Y T
var arr []int
func id(v *T) *T { return v }
func f(x *T) {
	_ = x
	_ = (x)
	_ = &x.F
	_ = (*x).F
	_ = x.F
	_ = x.G
	_ = Y.F
	_ = arr[0]
	_ = int(x.F)
	_ = id(x)
}
`)
	decl := findDecl(f, "f")
	var rhs []ast.Expr
	for _, s := range decl.Body.List {
		a, ok := s.(*ast.AssignStmt)
		if !ok {
			t.Fatalf("stmt not assign: %T", s)
		}
		rhs = append(rhs, a.Rhs[0])
	}
	keys := make([]Key, len(rhs))
	ok := make([]bool, len(rhs))
	for i, e := range rhs {
		keys[i], ok[i] = KeyOf(info, e)
	}
	for i := range 7 {
		if !ok[i] {
			t.Fatalf("rhs %d: KeyOf failed", i)
		}
	}
	if keys[0] != keys[1] {
		t.Fatalf("x and (x) differ: %v", keys[:2])
	}
	if keys[2] != keys[3] || keys[3] != keys[4] {
		t.Fatalf("&x.F, (*x).F, x.F differ: %v", keys[2:5])
	}
	if keys[0] == keys[4] {
		t.Fatalf("x equals x.F")
	}
	if keys[4] == keys[5] {
		t.Fatalf("x.F equals x.G")
	}
	if keys[4].Root == keys[6].Root {
		t.Fatalf("x.F and Y.F share root")
	}
	for i := 7; i < 10; i++ {
		if ok[i] {
			t.Fatalf("rhs %d: KeyOf should fail for index, conversion, call", i)
		}
	}
}

func TestReaches(t *testing.T) {
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
	c := 3
	return a + b + c
}
`)
	mayReturn := func(*ast.CallExpr) bool { return true }
	fdecl := findDecl(f, "f")
	fg := newGraph(fdecl, cfg.New(fdecl.Body, mayReturn))
	from := fdecl.Body.List[0]
	branch, ok := fdecl.Body.List[2].(*ast.IfStmt)
	if !ok {
		t.Fatalf("third stmt not if")
	}
	avoid := branch.Body.List[0]
	to := fdecl.Body.List[3]
	if !fg.Reaches(from, to, nil) {
		t.Fatalf("Reaches without avoid failed")
	}
	if !fg.Reaches(from, to, func(n ast.Node) bool { return n == avoid }) {
		t.Fatalf("Reaches around one branch failed")
	}
	gdecl := findDecl(f, "g")
	gg := newGraph(gdecl, cfg.New(gdecl.Body, mayReturn))
	ga, gb, gc := gdecl.Body.List[0], gdecl.Body.List[1], gdecl.Body.List[2]
	if !gg.Reaches(ga, gc, nil) {
		t.Fatalf("linear Reaches failed")
	}
	if gg.Reaches(ga, gc, func(n ast.Node) bool { return n == gb }) {
		t.Fatalf("Reaches crossed avoided node")
	}
	if gg.Reaches(gc, ga, nil) {
		t.Fatalf("Reaches ran backward")
	}
}

const joinSource = `package p
import "sync"
func f() int {
	var wg sync.WaitGroup
	s := ""
	wg.Add(1)
	go func() {
		defer wg.Done()
		s = "x"
	}()
	before := s
	wg.Wait()
	after := s
	return len(before) + len(after)
}
func g(c bool) int {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
	}()
	if c {
		wg.Wait()
	}
	x := 1
	return x
}
func h() int {
	done := make(chan struct{})
	go func() {
		defer close(done)
	}()
	a := 1
	<-done
	b := 2
	return a + b
}
func loop() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	wg.Wait()
}
`

func spawnOf(t *testing.T, pass *analysis.Pass, decl *ast.FuncDecl) Spawn {
	t.Helper()
	spawns := Spawns(pass, decl)
	if len(spawns) != 1 {
		t.Fatalf("%s: got %d spawns, want 1", decl.Name.Name, len(spawns))
	}
	return spawns[0]
}

func TestJoined(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, joinSource)
	pass := ctrlPass(t, pkg)
	decls := map[string]*ast.FuncDecl{}
	for _, d := range pkg.Syntax[0].Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls[fd.Name.Name] = fd
		}
	}
	fg := NewGraph(pass, decls["f"])
	if fg == nil {
		t.Fatalf("NewGraph f: nil")
	}
	fs := spawnOf(t, pass, decls["f"])
	if fs.Group.zero() {
		t.Fatalf("f: zero Group")
	}
	if Joined(pass, fg, fs, decls["f"].Body.List[4]) {
		t.Fatalf("f: joined before Wait")
	}
	if !Joined(pass, fg, fs, decls["f"].Body.List[6]) {
		t.Fatalf("f: not joined after Wait")
	}
	gg := NewGraph(pass, decls["g"])
	gs := spawnOf(t, pass, decls["g"])
	if Joined(pass, gg, gs, decls["g"].Body.List[4]) {
		t.Fatalf("g: joined when Wait is on one branch")
	}
	hg := NewGraph(pass, decls["h"])
	hs := spawnOf(t, pass, decls["h"])
	if Joined(pass, hg, hs, decls["h"].Body.List[2]) {
		t.Fatalf("h: joined before receive")
	}
	if !Joined(pass, hg, hs, decls["h"].Body.List[4]) {
		t.Fatalf("h: not joined after receive")
	}
	lg := NewGraph(pass, decls["loop"])
	ls := spawnOf(t, pass, decls["loop"])
	if !ls.InLoop {
		t.Fatalf("loop: spawn not in loop")
	}
	if ls.Group.zero() {
		t.Fatalf("loop: zero Group")
	}
	_ = lg
}

func TestHeldAt(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, `package p
import "sync"
func f(mu *sync.Mutex, c bool) int {
	mu.Lock()
	a := 1
	mu.Unlock()
	b := 2
	if c {
		mu.Lock()
	}
	d := 3
	mu.Unlock()
	return a + b + d
	var mu2 sync.Mutex
	mu2.Lock()
	go func() {
		mu.Lock()
	}()
	defer mu.Unlock()
}
func g(mu *sync.RWMutex) int {
	mu.RLock()
	a := 1
	mu.RUnlock()
	mu.TryLock()
	b := 2
	_ = b
	_ = mu.TryRLock()
	mu.Unlock()
	return a
}
`)
	pass := ctrlPass(t, pkg)
	decls := map[string]*ast.FuncDecl{}
	for _, d := range pkg.Syntax[0].Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls[fd.Name.Name] = fd
		}
	}
	fg := NewGraph(pass, decls["f"])
	held := HeldAt(pass, fg)
	got := held(decls["f"].Body.List[1])
	if len(got) != 1 || got[0].Root.Name() != "mu" || got[0].Path != "" {
		t.Fatalf("after Lock: got %v", got)
	}
	if held(decls["f"].Body.List[3]) != nil {
		t.Fatalf("after Unlock: held %v", held(decls["f"].Body.List[3]))
	}
	if held(decls["f"].Body.List[5]) != nil {
		t.Fatalf("after one-sided branch unlock: held")
	}
	gg := NewGraph(pass, decls["g"])
	gheld := HeldAt(pass, gg)
	if len(gheld(decls["g"].Body.List[1])) != 1 {
		t.Fatalf("after RLock: not held")
	}
	if gheld(decls["g"].Body.List[4]) != nil {
		t.Fatalf("after TryLock: held")
	}
}

const escapeSource = `package p
type T struct{ F int }
var G int
func ret(v int) int                 { return v }
func snd(ch chan int, v int)        { ch <- v }
func fld(s *T, v int)               { s.F = v }
func glob(v int)                    { G = v }
func mp(m map[string]int, v int)    { m["k"] = v }
func sl(s []int, v int)             { s[0] = v }
func use(v int)                     { _ = v }
func arg(v int)                     { use(v) }
func run(f func())                  { f() }
func capf(v int)                    { run(func() { _ = v }) }
func addr(v int)                    { p := &v; _ = p }
func addrfs(v T)                    { p := &v.F; _ = p }
func df(v int)                      { defer use(v) }
func gof(v int)                     { go use(v) }
func sel2(ch chan int, v int, c bool) {
	if c {
		ch <- v
		return
	}
	select {
	case ch <- v:
	case <-ch:
	}
}
func comp(v int) T            { return T{F: v} }
func sl2(v []int) []int      { return v[1:2] }
func deref(p *int) int       { return *p }
func g2[T any](v T)          { _ = v }
func gen[T any](v T)         { g2[T](v) }
func neg1(v int) int         { x := v; x++; return x }
func neg2(v int)             { v++; _ = v }
func (T) M() int             { return 1 }
func neg3(v T) int           { return v.M() }
func neg4(v []int) int       { n := 0; for range v { n++ }; return n }
func paren2(v int) int       { x := (v); x++; return x }
func bin2(v int) int         { x := v + 1; x++; return x }
func notf(v bool) bool       { x := !v; _ = x; return true }
func sl3(v []int) int        { return len(v[1:2]) }
func declv(v int) int        { var x = v; return x }
func many(v int, c bool, m map[string]int, ch chan int) int {
	if c {
		v = 1
	} else {
		v = 2
	}
	total := 0
	for i := 0; i < v; i++ {
		total += i
	}
	for k := range m {
		total += m[k]
	}
	switch v {
	case 1:
		total++
	default:
		total--
	}
	switch x := any(v).(type) {
	case int:
		total += x
	default:
		total--
	}
outer:
	for i := 0; i < 3; i++ {
		if i > 1 {
			break outer
		}
	}
	select {
	case y := <-ch:
		total += y
	default:
		total++
	}
	return total
}
`

func fieldVar(info *types.Info, field *ast.Field, pname string) *types.Var {
	for _, id := range field.Names {
		if id.Name != pname {
			continue
		}
		if obj, ok := info.Defs[id].(*types.Var); ok {
			return obj
		}
	}
	return nil
}

func escapeVar(info *types.Info, decl *ast.FuncDecl, pname string) *types.Var {
	for _, field := range decl.Type.Params.List {
		if v := fieldVar(info, field, pname); v != nil {
			return v
		}
	}
	return nil
}

func checkEscapeCase(t *testing.T, f *ast.File, info *types.Info, name string, want bool) {
	t.Helper()
	decl := findDecl(f, name)
	if decl == nil {
		t.Fatalf("%s: decl missing", name)
	}
	pname := "v"
	if name == "deref" {
		pname = "p"
	}
	v := escapeVar(info, decl, pname)
	if v == nil {
		t.Fatalf("%s: param missing", name)
	}
	if got := Escapes(info, decl, v); got != want {
		t.Fatalf("%s: Escapes = %v, want %v", name, got, want)
	}
}

func TestEscapes(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, escapeSource)
	want := map[string]bool{
		"ret": true, "snd": true, "fld": true, "glob": true, "mp": true,
		"sl": true, "arg": true, "capf": true, "addr": true, "addrfs": true,
		"df": true, "gof": true, "sel2": true, "comp": true, "sl2": true,
		"deref": false, "gen": true, "neg1": false, "neg2": false,
		"neg3": false, "neg4": false, "paren2": false, "bin2": false,
		"notf": false, "sl3": false, "declv": false, "many": false,
	}
	for name, w := range want {
		checkEscapeCase(t, f, info, name, w)
	}
}

func TestStraightLine(t *testing.T) {
	t.Parallel()
	f, _ := checkTypes(t, `package p
func f(c bool) int {
	a := 1
	b := 2
	if c {
		c = false
	}
	d := 3
	e := 4
	return a + b + d + e
}
`)
	decl := findDecl(f, "f")
	list := decl.Body.List
	if !StraightLine(decl, list[0], list[1]) {
		t.Fatalf("adjacent statements not straight")
	}
	if StraightLine(decl, list[1], list[3]) {
		t.Fatalf("straight across if")
	}
	if StraightLine(decl, list[0], list[4]) {
		t.Fatalf("straight across if to return")
	}
	innerBody, ok := list[2].(*ast.IfStmt)
	if !ok {
		t.Fatalf("third stmt not if")
	}
	inner := innerBody.Body.List[0]
	if StraightLine(decl, list[0], inner) {
		t.Fatalf("straight into nested list")
	}
	if StraightLine(decl, list[0], nil) {
		t.Fatalf("nil statement straight")
	}
}

func TestEndsInReturn(t *testing.T) {
	t.Parallel()
	ret := []ast.Stmt{&ast.ExprStmt{}, &ast.ReturnStmt{}}
	if !EndsInReturn(ret) {
		t.Fatalf("return list missed")
	}
	if EndsInReturn(ret[:1]) {
		t.Fatalf("expr list ends in return")
	}
	if EndsInReturn(nil) {
		t.Fatalf("empty list ends in return")
	}
}

func TestIsCall(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, `package p
func g(s string) int { return 1 }
func f(s string, b []byte) {
	_ = len(s)
	_ = cap(b)
	_ = string(b)
	_ = g(s)
}
`)
	decl := findDecl(f, "f")
	var rhs []ast.Expr
	for _, s := range decl.Body.List {
		a, ok := s.(*ast.AssignStmt)
		if !ok {
			t.Fatalf("stmt not assign: %T", s)
		}
		rhs = append(rhs, a.Rhs[0])
	}
	for i := range 3 {
		if _, ok := IsCall(info, rhs[i]); ok {
			t.Fatalf("rhs %d: builtin or conversion is a call", i)
		}
	}
	if _, ok := IsCall(info, rhs[3]); !ok {
		t.Fatalf("plain call missed")
	}
	if _, ok := IsCall(nil, rhs[0]); ok {
		t.Fatalf("len without info is a call")
	}
}

func TestIsLocal(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, `package p
var GV int
func f(p int) (r int) {
	x := 1
	_ = x + p + r + GV
	g := func() {
		y := 2
		_ = y
	}
	_ = g
	return r
}
`)
	decl := findDecl(f, "f")
	byName := func(name string) *types.Var {
		for id, obj := range info.Defs {
			if v, ok := obj.(*types.Var); ok && id.Name == name {
				return v
			}
		}
		return nil
	}
	if IsLocal(info, decl, byName("p")) {
		t.Fatalf("param is local")
	}
	if IsLocal(info, decl, byName("r")) {
		t.Fatalf("result is local")
	}
	if !IsLocal(info, decl, byName("x")) {
		t.Fatalf("body var not local")
	}
	if IsLocal(info, decl, byName("GV")) {
		t.Fatalf("global is local")
	}
	if IsLocal(info, decl, byName("y")) {
		t.Fatalf("literal var is local to decl")
	}
	if IsLocal(nil, decl, byName("x")) {
		t.Fatalf("nil info is local")
	}
}

func TestDefinedBy(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, `package p
type T struct{ F int }
func f(m map[string]T, k string) {
	v := m[k]
	_ = v
	w := 1
	w++
	_ = w
	u := 2
	u = 3
	_ = u
	s := T{F: 1}
	s.F = 2
	_ = s
}
`)
	decl := findDecl(f, "f")
	byName := func(name string) *types.Var {
		for id, obj := range info.Defs {
			if v, ok := obj.(*types.Var); ok && id.Name == name {
				return v
			}
		}
		return nil
	}
	if _, ok := DefinedBy(info, decl, byName("v")); !ok {
		t.Fatalf("v not defined")
	}
	if _, ok := DefinedBy(info, decl, byName("w")); ok {
		t.Fatalf("w defined despite ++")
	}
	if _, ok := DefinedBy(info, decl, byName("u")); ok {
		t.Fatalf("u defined despite assignment")
	}
	if _, ok := DefinedBy(info, decl, byName("s")); !ok {
		t.Fatalf("s not defined despite field write")
	}
	if _, ok := DefinedBy(info, decl, byName("k")); ok {
		t.Fatalf("param defined")
	}
}

func TestMethodKey(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, `package p
type M struct{}
func (m *M) Lock()   {}
func (m *M) Unlock() {}
type S struct{ mu M }
func (s *S) g() { s.mu.Lock() }
type T struct{ M }
func (t *T) h() { t.Lock() }
`)
	var calls []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			calls = append(calls, c)
		}
		return true
	})
	if len(calls) != 2 {
		t.Fatalf("got %d calls", len(calls))
	}
	k, ok := MethodKey(info, calls[0])
	if !ok || k.Root.Name() != "s" || k.Path != ".mu" {
		t.Fatalf("direct key: %v %v", k, ok)
	}
	k, ok = MethodKey(info, calls[1])
	if !ok || k.Root.Name() != "t" || k.Path != ".M" {
		t.Fatalf("promoted key: %v %v", k, ok)
	}
	if _, ok := MethodKey(nil, calls[0]); ok {
		t.Fatalf("nil info key")
	}
}

func TestNewGraph(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	src := "package p\nfunc stub()\nfunc full() {}\n"
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Types:      map[ast.Expr]types.TypeAndValue{},
	}
	ires, err := inspect.Analyzer.Run(&analysis.Pass{Files: []*ast.File{f}})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	cres, err := ctrlflow.Analyzer.Run(&analysis.Pass{
		Files:            []*ast.File{f},
		Fset:             fset,
		TypesInfo:        info,
		ResultOf:         map[*analysis.Analyzer]any{inspect.Analyzer: ires},
		ImportObjectFact: func(types.Object, analysis.Fact) bool { return false },
		ExportObjectFact: func(types.Object, analysis.Fact) {},
	})
	if err != nil {
		t.Fatalf("ctrlflow: %v", err)
	}
	pass := &analysis.Pass{
		Files:     []*ast.File{f},
		Fset:      fset,
		TypesInfo: info,
		ResultOf:  map[*analysis.Analyzer]any{ctrlflow.Analyzer: cres},
	}
	if NewGraph(pass, findDecl(f, "stub")) != nil {
		t.Fatalf("bodyless decl has graph")
	}
	if NewGraph(pass, &ast.BlockStmt{}) != nil {
		t.Fatalf("block has graph")
	}
	bare := &analysis.Pass{Files: []*ast.File{f}, Fset: fset, TypesInfo: info}
	if NewGraph(bare, findDecl(f, "full")) != nil {
		t.Fatalf("graph without ctrlflow result")
	}
	if newGraph(nil, nil) != nil {
		t.Fatalf("nil cfg has graph")
	}
}

func TestMethodValueHasNoKey(t *testing.T) {
	t.Parallel()
	f, info := checkTypes(t, "package p\nimport \"sync\"\nfunc f(wg sync.WaitGroup) { _ = wg.Done }\n")
	decl := findDecl(f, "f")
	var sel *ast.SelectorExpr
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok && s.Sel.Name == "Done" {
			sel = s
		}
		return true
	})
	if sel == nil {
		t.Fatalf("Done selector missing")
	}
	if _, ok := KeyOf(info, sel); ok {
		t.Fatalf("method value has key")
	}
}

func TestRLockIsClassedAsRLock(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nfunc f(mu *sync.RWMutex) { mu.RLock(); _ = mu }\n")
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	var call *ast.CallExpr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr); ok && s.Sel.Name == "RLock" {
				call = c
			}
		}
		return true
	})
	if call == nil {
		t.Fatalf("RLock call missing")
	}
	if got := mutexMethod(typeutil.StaticCallee(pass.TypesInfo, call)); got != "RLock" {
		t.Fatalf("mutexMethod = %q, want RLock", got)
	}
}

func TestGoLockDoesNotHold(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nfunc f(mu *sync.Mutex) {\nmu.Lock()\nmu.Unlock()\nfunc() {\ngo mu.Lock()\n}()\n_ = 1\n}\n")
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	held := HeldAt(pass, NewGraph(pass, fd))
	if got := held(fd.Body.List[3]); len(got) != 0 {
		t.Fatalf("go lock held: %v", got)
	}
}

func TestSpawnedUnlockDoesNotRelease(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, "package p\nimport \"sync\"\nfunc f(mu *sync.Mutex) {\nfunc() {\nmu.Lock()\ngo mu.Unlock()\n}()\n_ = 1\n}\n")
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	held := HeldAt(pass, NewGraph(pass, fd))
	if got := held(fd.Body.List[1]); len(got) != 1 {
		t.Fatalf("spawn unlock released: %v", got)
	}
}
