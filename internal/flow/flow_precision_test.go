package flow

import (
	"go/ast"
	"go/types"
	"testing"
)

func escapeOf(t *testing.T, src, name, param string) bool {
	t.Helper()
	f, info := checkTypes(t, src)
	decl := findDecl(f, name)
	if decl == nil {
		t.Fatalf("%s: decl missing", name)
	}
	var v *types.Var
	for _, field := range decl.Type.Params.List {
		for _, id := range field.Names {
			if id.Name == param {
				if vv, ok := info.Defs[id].(*types.Var); ok {
					v = vv
				}
			}
		}
	}
	if v == nil {
		t.Fatalf("%s: param %s missing", name, param)
	}
	return Escapes(info, decl, v)
}

func TestEscapesConversionAlias(t *testing.T) {
	t.Parallel()
	src := `package p
type Ch chan int
func convAlias(ch chan int) Ch { return Ch(ch) }
func convDrop(v int) any { return any(v) }
`
	if !escapeOf(t, src, "convAlias", "ch") {
		t.Fatalf("channel conversion does not escape")
	}
	if escapeOf(t, src, "convDrop", "v") {
		t.Fatalf("int to any conversion escapes")
	}
}

func TestEscapesSliceCompositeAlias(t *testing.T) {
	t.Parallel()
	src := `package p
type Job struct{ Out chan int }
func sliceAlias(buf []byte) []byte { view := buf[:4]; return view }
func compAlias(ch chan int) Job { job := Job{Out: ch}; return job }
`
	if !escapeOf(t, src, "sliceAlias", "buf") {
		t.Fatalf("slice alias does not escape")
	}
	if !escapeOf(t, src, "compAlias", "ch") {
		t.Fatalf("composite alias does not escape")
	}
}

func TestEscapesAssertAndArraySlice(t *testing.T) {
	t.Parallel()
	src := `package p
var registry any
func assertAlias(x any) { registry = x.(chan int) }
type S struct{ buf [8]byte }
var keep any
func arraySlice(s S) { keep = s.buf[:] }
`
	if !escapeOf(t, src, "assertAlias", "x") {
		t.Fatalf("type assertion does not escape")
	}
	if !escapeOf(t, src, "arraySlice", "s") {
		t.Fatalf("array field slice does not escape")
	}
}

func TestEscapesInPlaceReturn(t *testing.T) {
	t.Parallel()
	src := `package p
var registry chan int
func inPlaceRet(ch chan int) { registry = func() chan int { return ch }() }
`
	if !escapeOf(t, src, "inPlaceRet", "ch") {
		t.Fatalf("in-place literal return does not escape")
	}
}

func TestEscapesImplicitAddr(t *testing.T) {
	t.Parallel()
	src := `package p
import "sync"
var registry *Buf
type Buf struct{ n int }
func (b *Buf) Register() { registry = b }
func callReg() { var b Buf; b.Register() }
func callLock() { var mu sync.Mutex; mu.Lock(); _ = mu }
`
	f, info := checkTypes(t, src)
	reg := findDecl(f, "callReg")
	b := firstLocal(info, reg)
	if b == nil {
		t.Fatalf("b missing")
	}
	if !Escapes(info, reg, b) {
		t.Fatalf("pointer receiver call does not escape")
	}
	callLock := findDecl(f, "callLock")
	mu := firstLocal(info, callLock)
	if mu == nil {
		t.Fatalf("mu missing")
	}
	if Escapes(info, callLock, mu) {
		t.Fatalf("sync lock escapes")
	}
}

func firstLocal(info *types.Info, decl *ast.FuncDecl) *types.Var {
	ds, ok := decl.Body.List[0].(*ast.DeclStmt)
	if !ok {
		return nil
	}
	gd, ok := ds.Decl.(*ast.GenDecl)
	if !ok {
		return nil
	}
	vs, ok := gd.Specs[0].(*ast.ValueSpec)
	if !ok {
		return nil
	}
	if v, ok := info.Defs[vs.Names[0]].(*types.Var); ok {
		return v
	}
	return nil
}

func TestGoGroupIgnoresInner(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, `package p
import "sync"
func f() {
var wg sync.WaitGroup
s := "a"
wg.Add(1)
go func() {
defer wg.Done()
var inner sync.WaitGroup
inner.Add(1)
inner.Done()
println(s)
}()
wg.Wait()
s = "b"
}
`)
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	sp := Spawns(pass, fd)[0]
	if sp.Group.zero() {
		t.Fatalf("outer group lost to inner group")
	}
	if sp.Group.Root.Name() != "wg" {
		t.Fatalf("group root = %v, want wg", sp.Group.Root)
	}
	g := NewGraph(pass, fd)
	if !Joined(pass, g, sp, fd.Body.List[5]) {
		t.Fatalf("outer Wait does not join after inner fix")
	}
}

func TestJoinedOpaqueHelper(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, `package p
import "sync"
func drain(wg *sync.WaitGroup) { wg.Wait() }
func f() string {
var wg sync.WaitGroup
s := "a"
wg.Add(1)
go func() { defer wg.Done(); println(s) }()
drain(&wg)
s = "b"
return s
}
`)
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	sp := Spawns(pass, fd)[0]
	g := NewGraph(pass, fd)
	if !Joined(pass, g, sp, fd.Body.List[5]) {
		t.Fatalf("helper call does not join")
	}
}

func TestJoinedOpaqueClosure(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, `package p
func f() string {
done := make(chan struct{})
s := "a"
go func() { defer close(done); println(s) }()
wait := func() { <-done }
wait()
s = "b"
return s
}
`)
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	sp := Spawns(pass, fd)[0]
	g := NewGraph(pass, fd)
	if !Joined(pass, g, sp, fd.Body.List[5]) {
		t.Fatalf("closure call does not join")
	}
}

func TestHeldAtInPlaceLiteral(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, `package p
import "sync"
var mu sync.Mutex
var m map[int]int
func f() {
func() { mu.Lock() }()
m[1] = 2
mu.Unlock()
}
`)
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	held := HeldAt(pass, NewGraph(pass, fd))
	got := held(fd.Body.List[1])
	if len(got) != 1 {
		t.Fatalf("in-place lock not held: %v", got)
	}
}

func localNamed(info *types.Info, decl *ast.FuncDecl, name string) *types.Var {
	var out *types.Var
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v, ok := info.Defs[id].(*types.Var); ok && v.Name() == name {
			out = v
			return false
		}
		return true
	})
	return out
}

func TestEscapesUnrelatedPointerCall(t *testing.T) {
	t.Parallel()
	src := `package p
import "bytes"
func f() {
var ch chan int
ch = make(chan int)
var b bytes.Buffer
b.WriteString("x")
_ = ch
_ = b
}
`
	f, info := checkTypes(t, src)
	decl := findDecl(f, "f")
	if decl == nil {
		t.Fatalf("f missing")
	}
	ch := localNamed(info, decl, "ch")
	b := localNamed(info, decl, "b")
	if ch == nil || b == nil {
		t.Fatalf("locals missing")
	}
	if !Escapes(info, decl, b) {
		t.Fatalf("b should escape through Register")
	}
	if Escapes(info, decl, ch) {
		t.Fatalf("unrelated ch escapes through b.WriteString")
	}
}

func TestEscapesConversionRefType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		src   string
		fn    string
		param string
		want  bool
	}{
		{"sliceToString", "package p\nfunc copyStr(buf []byte) string { s := string(buf); return s }\n", "copyStr", "buf", false},
		{"chanConvert", "package p\ntype Ch chan int\nfunc convAlias(ch chan int) Ch { return Ch(ch) }\n", "convAlias", "ch", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := escapeOf(t, tc.src, tc.fn, tc.param); got != tc.want {
				t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestHeldAtIIFEDeferredUnlock(t *testing.T) {
	t.Parallel()
	pkg := loadPackage(t, `package p
import "sync"
func f() {
var mu sync.Mutex
_ = func() int { mu.Lock(); defer mu.Unlock(); return 1 }()
m := 1
_ = m
}
`)
	pass := ctrlPass(t, pkg)
	var fd *ast.FuncDecl
	for _, d := range pkg.Syntax[0].Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
			fd = f
		}
	}
	held := HeldAt(pass, NewGraph(pass, fd))
	got := held(fd.Body.List[2])
	if len(got) != 0 {
		t.Fatalf("deferred unlock in IIFE still held: %v", got)
	}
}

func TestOpaqueJoinNegatives(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{"builtinLen", "package p\nfunc f() string {\ndone := make(chan struct{})\ns := \"a\"\ngo func() { defer close(done); println(s) }()\nif len(done) > 0 { println(\"pending\") }\ns = \"b\"\nreturn s\n}\n"},
		{"noopFunc", "package p\nimport \"sync\"\nfunc noop(wg *sync.WaitGroup) {}\nfunc f() string {\nvar wg sync.WaitGroup\ns := \"a\"\nwg.Add(1)\ngo func() { defer wg.Done(); println(s) }()\nnoop(&wg)\ns = \"b\"\nreturn s\n}\n"},
		{"condClosure", "package p\nimport \"sync\"\nfunc f() string {\nvar wg sync.WaitGroup\ns := \"a\"\nwg.Add(1)\ngo func() { defer wg.Done(); println(s) }()\nwait := func() { if s != \"\" { wg.Wait() } }\nwait()\ns = \"b\"\nreturn s\n}\n"},
		{"signalNoop", "package p\nfunc watch(ch chan struct{}) {}\nfunc f() string {\ndone := make(chan struct{})\ns := \"a\"\ngo func() { defer close(done); println(s) }()\nwatch(done)\ns = \"b\"\nreturn s\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkg := loadPackage(t, tc.src)
			pass := ctrlPass(t, pkg)
			var fd *ast.FuncDecl
			for _, d := range pkg.Syntax[0].Decls {
				if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "f" {
					fd = f
				}
			}
			if fd == nil {
				t.Fatalf("f missing")
			}
			var target ast.Node
			for _, s := range fd.Body.List {
				if a, ok := s.(*ast.AssignStmt); ok && len(a.Lhs) == 1 {
					if id, ok := a.Lhs[0].(*ast.Ident); ok && id.Name == "s" {
						target = s
					}
				}
			}
			if target == nil {
				t.Fatalf("target missing")
			}
			sp := Spawns(pass, fd)[0]
			g := NewGraph(pass, fd)
			if Joined(pass, g, sp, target) {
				t.Fatalf("%s: opaque call joins", tc.name)
			}
		})
	}
}
