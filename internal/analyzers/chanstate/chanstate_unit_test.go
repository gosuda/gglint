package chanstate

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/flow"
)

func testCheck(t *testing.T, src string) (*analysis.Pass, *types.Info, []*ast.FuncDecl) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Types:      map[ast.Expr]types.TypeAndValue{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	cfg := types.Config{Importer: importer.Default()}
	pkg, err := cfg.Check("p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	insp := inspector.New([]*ast.File{f})
	pass := &analysis.Pass{
		Fset:      fset,
		Files:     []*ast.File{f},
		Pkg:       pkg,
		TypesInfo: info,
		Report:    func(analysis.Diagnostic) {},
		ResultOf:  map[*analysis.Analyzer]any{inspect.Analyzer: insp},
	}
	cf, err := ctrlflow.Analyzer.Run(pass)
	if err != nil {
		t.Fatal(err)
	}
	pass.ResultOf[ctrlflow.Analyzer] = cf
	var decls []*ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			decls = append(decls, fd)
		}
	}
	return pass, info, decls
}

func testVar(t *testing.T, info *types.Info, fn ast.Node, name string) flow.Key {
	t.Helper()
	var found *types.Var
	ast.Inspect(fn, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != name {
			return true
		}
		if v, ok := info.Defs[id].(*types.Var); ok {
			found = v
			return false
		}
		if v, ok := info.Uses[id].(*types.Var); ok {
			found = v
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("no variable %s", name)
	}
	return flow.Key{Root: found}
}

func runCheck(t *testing.T, src string, check func(*analysis.Pass, ast.Node, *flow.Graph)) []string {
	t.Helper()
	pass, _, decls := testCheck(t, src)
	var out []string
	pass.Report = func(d analysis.Diagnostic) { out = append(out, d.Message) }
	for _, fd := range decls {
		g := flow.NewGraph(pass, fd)
		if g == nil {
			continue
		}
		check(pass, fd, g)
	}
	return out
}

func hasSub(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func TestUnderSelectCases(t *testing.T) {
	t.Parallel()
	_, _, decls := testCheck(t, "package p\nfunc f(ch chan int) {\nselect {\ncase ch <- 1:\ndefault:\n}\nch <- 2\n}\n")
	fn := decls[0]
	var sends []ast.Node
	ast.Inspect(fn, func(n ast.Node) bool {
		if s, ok := n.(*ast.SendStmt); ok {
			sends = append(sends, s)
		}
		return true
	})
	if len(sends) != 2 {
		t.Fatalf("want 2 sends, got %d", len(sends))
	}
	if !underSelect(fn, sends[0]) {
		t.Error("select case send is not under a select")
	}
	if underSelect(fn, sends[1]) {
		t.Error("plain send is under a select")
	}
	if withinNode(sends[1], sends[0]) {
		t.Error("sibling contains sibling")
	}
}

func TestLoopTripTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int64
		ok   bool
	}{
		{"count", "package p\nfunc f() {\nfor i := 0; i < 3; i++ {\n_ = i\n}\n}\n", 3, true},
		{"rangeConst", "package p\nfunc f() {\nfor range 5 {\n_ = 1\n}\n}\n", 5, true},
		{"rangeVar", "package p\nfunc f(xs []int) {\nfor range xs {\n_ = 1\n}\n}\n", 0, false},
		{"nonConst", "package p\nfunc f(n int) {\nfor i := 0; i < n; i++ {\n_ = i\n}\n}\n", 0, false},
		{"nonZero", "package p\nfunc f() {\nfor i := 1; i < 3; i++ {\n_ = i\n}\n}\n", 0, false},
		{"nonLess", "package p\nfunc f() {\nfor i := 0; i <= 3; i++ {\n_ = i\n}\n}\n", 0, false},
		{"noPost", "package p\nfunc f() {\nfor i := 0; i < 3; {\n_ = i\n}\n}\n", 0, false},
		{"cond", "package p\nfunc f() {\nfor true {\n_ = 1\n}\n}\n", 0, false},
	}
	for _, c := range cases {
		_, info, decls := testCheck(t, c.src)
		var st ast.Stmt
		ast.Inspect(decls[0], func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ForStmt:
				st = n
			case *ast.RangeStmt:
				st = n
			}
			return st == nil
		})
		if st == nil {
			t.Fatalf("%s: no loop", c.name)
		}
		got, ok := loopTrip(info, st)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: got %d,%v want %d,%v", c.name, got, ok, c.want, c.ok)
		}
		if loopBody(st) == nil {
			t.Errorf("%s: no loop body", c.name)
		}
	}
	if loopBody(&ast.EmptyStmt{}) != nil {
		t.Error("loop body of a non-loop")
	}
	if bodyOf(nil) != nil {
		t.Error("body of nothing")
	}
}

func TestLoopExitsTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"plain", "_ = v", false},
		{"ret", "_ = v\nreturn", true},
		{"brk", "if v > 0 {\nbreak\n}", true},
		{"cont", "if v > 0 {\ncontinue\n}\n_ = v", false},
		{"got", "if v > 0 {\ngoto done\n}\n_ = v\ndone:\n_ = v", true},
		{"nest", "for i := 0; i < 3; i++ {\nbreak\n}\n_ = v", false},
		{"tsRet", "switch v := any(v).(type) {\ncase int:\n_ = v\nreturn\n}", true},
		{"nestLbl", "out:\nfor i := 0; i < 3; i++ {\nbreak out\n}\n_ = v", true},
		{"swBrk", "switch v {\ncase 1:\nbreak\ndefault:\n_ = v\n}", false},
		{"swRet", "switch v {\ncase 1:\nreturn\ndefault:\n_ = v\n}", true},
		{"selRet", "select {\ncase x := <-ch:\n_ = x\nreturn\ndefault:\n_ = v\n}", true},
		{"tsRet", "switch v := any(v).(type) {\ncase int:\n_ = v\nreturn\n}", true},
		{"ifElse", "if v > 0 {\nreturn\n} else {\nreturn\n}", true},
		{"ifInit", "if t := v; t > 0 {\nreturn\n}", true},
	}
	for _, c := range cases {
		src := "package p\nfunc f(ch chan int) {\nfor v := range ch {\n" + c.body + "\n}\n}\n"
		_, info, decls := testCheck(t, src)
		var rs *ast.RangeStmt
		ast.Inspect(decls[0], func(n ast.Node) bool {
			if r, ok := n.(*ast.RangeStmt); ok {
				rs = r
			}
			return rs == nil
		})
		if rs == nil {
			t.Fatalf("%s: no range", c.name)
		}
		_ = info
		if got := loopExits(rs.Body.List); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if loopExits(nil) {
		t.Error("empty list exits")
	}
}

func TestHandsOnTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"ret", "package p\nfunc f(ch chan int) any {\nreturn ch\n}\n", true},
		{"sendv", "package p\nfunc f(ch chan int, other chan chan int) {\nother <- ch\n}\n", true},
		{"glob", "package p\nvar sink any\nfunc f(ch chan int) {\nsink = ch\n}\n", true},
		{"field", "package p\ntype box struct{ C chan int }\nfunc f(ch chan int, b *box) {\nb.C = ch\n}\n", true},
		{"index", "package p\nfunc f(ch chan int, m map[int]chan int) {\nm[0] = ch\n}\n", true},
		{"addr", "package p\nfunc f(ch chan int) {\n_ = &ch\n}\n", true},
		{"caplit", "package p\nfunc f(ch chan int) {\nh := func() {\nprintln(ch)\n}\n_ = h\n}\n", true},
		{"spawn", "package p\nfunc f(ch chan int) {\ngo func() {\nch <- 1\n}()\n}\n", false},
		{"plain", "package p\nfunc f(ch chan int) {\nch <- 1\n<-ch\n}\n", false},
		{"close", "package p\nfunc f(ch chan int) {\nclose(ch)\n}\n", false},
		{"len", "package p\nfunc f(ch chan int) {\n_ = len(ch)\n}\n", false},
		{"conv", "package p\nfunc f(ch chan int) {\n_ = (<-chan int)(ch)\n}\n", false},
		{"local", "package p\nfunc f(ch chan int) {\nx := ch\n_ = x\n}\n", false},
		{"gocall", "package p\nfunc work() {}\nfunc f(ch chan int) {\ngo work()\n_ = ch\n}\n", false},
		{"litcall", "package p\nvar sink any\nfunc f(ch chan int) {\nsink = func() int {\nreturn 1\n}\n}\n", false},
		{"litval", "package p\nfunc f(ch chan int) {\nh := func() int {\nreturn 1\n}\n_ = h\n_ = ch\n}\n", false},
		{"star", "package p\nfunc f(ch chan int, p *int) {\n_ = *p\n_ = ch\n}\n", false},
		{"not", "package p\nfunc f(a bool, ch chan int) {\n_ = !a\n_ = ch\n}\n", false},
		{"sel", "package p\ntype box struct{ C int }\nfunc f(b box, ch chan int) {\n_ = b.C\n_ = ch\n}\n", false},
		{"idx", "package p\nfunc f(xs []int, ch chan int) {\n_ = xs[0]\n_ = ch\n}\n", false},
		{"slice", "package p\nfunc f(xs []int, ch chan int) {\n_ = xs[1:2]\n_ = ch\n}\n", false},
		{"comp", "package p\ntype box struct{ C int }\nfunc f(ch chan int) {\n_ = box{C: 1}\n_ = ch\n}\n", false},
		{"assert", "package p\nfunc f(a any, ch chan int) {\n_ = a.(int)\n_ = ch\n}\n", false},
		{"generic", "package p\nfunc ident[T any](v T) T {\nreturn v\n}\nfunc f(n int, ch chan int) {\n_ = ident[int](n)\n_ = ch\n}\n", false},
		{"variadic", "package p\nfunc sum(ns ...int) int {\nreturn len(ns)\n}\nfunc f(xs []int, ch chan int) {\n_ = sum(xs...)\n_ = ch\n}\n", false},
		{"rich", "package p\nfunc f(ch chan int, xs []int) int {\nn := 0\nfor _, x := range xs {\n_ = x\nswitch x {\ncase 1:\nselect {\ncase v := <-ch:\nn += v\ndefault:\nn++\n}\ndefault:\nn--\n}\nif n > 10 {\nreturn n\n}\nn++\n}\n_ = n\nreturn n\n}\n", false},
	}
	for _, c := range cases {
		_, info, decls := testCheck(t, c.src)
		fn := decls[len(decls)-1]
		if got := handsOn(info, fn, testVar(t, info, fn, "ch")); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestChanOpsWalk(t *testing.T) {
	t.Parallel()
	src := "package p\nfunc ident[T any](v T) T {\nreturn v\n}\nfunc sum(ns ...int) int {\nreturn len(ns)\n}\ntype box struct{ C int }\nfunc rich(a bool, n int, ch chan int, xs []int, p *int, b box) int {\nvar made = make(chan int, 2)\n_ = made\nch = made\nn++\nif t := n; t > 0 {\n_ = t\n}\nfor _, x := range xs {\n_ = x\nswitch x {\ncase 1:\nselect {\ncase made <- x:\nn += x\ncase v := <-ch:\n_ = v\ndefault:\nn--\n}\ndefault:\nn += 2\n}\n}\nfor v := range made {\n_ = v\nbreak\n}\nswitch v := any(n).(type) {\ncase int:\n_ = v\n}\ndefer func() {\n_ = n\n}()\ngo func() {\nmade <- 1\n}()\n_ = ident[int](n)\n_ = n + 1\n_ = (n)\n_ = *p\n_ = len(xs)\n_ = int64(n)\n_ = sum(xs...)\n_ = b.C\n_ = xs[1:2]\n_ = box{C: n}\nif a {\nclose(ch)\n}\nreturn n\n}\n"
	_, info, decls := testCheck(t, src)
	fn := decls[len(decls)-1]
	k := testVar(t, info, fn, "ch")
	ops := chanOps(info, fn, k, opSend|opRecv|opClose|opAssign|opDefine)
	if len(ops) != 2 {
		t.Fatalf("want 2 ops, got %d", len(ops))
	}
	onlyRecv := chanOps(info, fn, k, opRecv)
	if len(onlyRecv) != 0 {
		t.Fatalf("want 0 recvs outside select, got %d", len(onlyRecv))
	}
	if got := hasRange(info, fn, testVar(t, info, fn, "made")); !got {
		t.Error("missing range over made")
	}
	if hasRange(info, fn, k) {
		t.Error("phantom range over ch")
	}
}

func TestMadeCapVariants(t *testing.T) {
	t.Parallel()
	_, info, decls := testCheck(t, "package p\nfunc f() {\nvar ch = make(chan int, 2)\n_ = ch\n}\n")
	if got, ok := madeCap(info, decls[0], testVar(t, info, decls[0], "ch")); !ok || got != 2 {
		t.Errorf("valued make: got %d,%v", got, ok)
	}
	_, info, decls = testCheck(t, "package p\nfunc f(ch chan int, n int) {\nvar other = make(chan int, n)\n_ = ch\n_ = other\n}\n")
	if _, ok := madeCap(info, decls[0], testVar(t, info, decls[0], "other")); ok {
		t.Error("non-constant capacity counts")
	}
	_, info, decls = testCheck(t, "package p\nfunc f(a bool) {\nvar ch chan int\nif a {\nch = make(chan int)\n} else {\nch = make(chan int, 1)\n}\n_ = ch\n}\n")
	if _, ok := madeCap(info, decls[0], testVar(t, info, decls[0], "ch")); ok {
		t.Error("plain assignment counts as a make definition")
	}
}

func TestNilVarVariants(t *testing.T) {
	t.Parallel()
	_, info, decls := testCheck(t, "package p\nfunc f() {\nvar ch chan int\nch = make(chan int, 1)\n_ = ch\n}\n")
	if got := nilLocals(info, decls[0]); len(got) != 0 {
		t.Errorf("assigned var counts as nil: %d", len(got))
	}
	_, info, decls = testCheck(t, "package p\nfunc f() {\nvar ch = make(chan int)\n_ = ch\n}\n")
	if got := nilLocals(info, decls[0]); len(got) != 0 {
		t.Errorf("valued var counts as nil: %d", len(got))
	}
	_, info, decls = testCheck(t, "package p\nfunc f() {\nvar ch chan int\nn := 0\nn++\n_ = n\nch <- 1\n}\n")
	if got := nilLocals(info, decls[0]); len(got) != 1 {
		t.Errorf("want 1 nil local, got %d", len(got))
	}
	_, info, decls = testCheck(t, "package p\nfunc f() {\nvar a, b chan int\n_ = a\n_ = b\n}\n")
	if got := nilLocals(info, decls[0]); len(got) != 2 {
		t.Errorf("want 2 nil locals, got %d", len(got))
	}
}

func TestStepJoin(t *testing.T) {
	t.Parallel()
	if joinState(stOpen, stOpen) != stOpen {
		t.Error("same states stay")
	}
	if joinState(stOpen, stClosed) != stUnknown {
		t.Error("different states merge to unknown")
	}
	if stepOp(chanOp{Kind: opClose}, stOpen) != stClosed {
		t.Error("close closes")
	}
	if stepOp(chanOp{Kind: opAssign}, stOpen) != stUnknown {
		t.Error("assign blurs")
	}
	if stepOp(chanOp{Kind: opSend}, stOpen) != stOpen {
		t.Error("send keeps")
	}
	if defState(chanOp{Kind: opDefine, Make: true}) != stOpen {
		t.Error("make opens")
	}
	if defState(chanOp{Kind: opDefine, NilInit: true}) != stNil {
		t.Error("bare var is nil")
	}
	if defState(chanOp{Kind: opDefine}) != stUnknown {
		t.Error("other defines blur")
	}
	if isChan(nil) {
		t.Error("nothing is a channel")
	}
	if isMake(nil, nil) {
		t.Error("nothing is a make")
	}
	if _, ok := constInt(nil, nil); ok {
		t.Error("nothing is constant")
	}
	if _, ok := keyOfIdent(nil, nil); ok {
		t.Error("nothing resolves a key")
	}
	_, info, decls := testCheck(t, "package p\nfunc f(ch chan int) {\n_ = ch\n}\n")
	if got := chanName(testVar(t, info, decls[0], "ch")); got != "ch" {
		t.Errorf("want ch, got %q", got)
	}
	if loopBody(nil) != nil {
		t.Error("nothing has no body")
	}
}

func TestLiteralTouchCases(t *testing.T) {
	t.Parallel()
	_, info, decls := testCheck(t, "package p\nfunc f(ch chan int, n int) {\ngo func() {\nch <- 1\n}()\n_ = ch\n_ = n\n}\n")
	fn := decls[0]
	k := testVar(t, info, fn, "ch")
	other := testVar(t, info, fn, "n")
	var lits []*ast.FuncLit
	ast.Inspect(fn, func(n ast.Node) bool {
		if l, ok := n.(*ast.FuncLit); ok {
			lits = append(lits, l)
		}
		return true
	})
	if len(lits) == 0 {
		t.Fatal("no literal")
	}
	if !literalTouch(info, fn, k) {
		t.Error("literal mention missed")
	}
	if literalTouch(info, lits[0].Body.List[0], k) {
		t.Error("send statement is not a literal")
	}
	if mentionsKey(info, lits[0], other) {
		t.Error("phantom mention")
	}
}

func TestStraightRecvsSkip(t *testing.T) {
	t.Parallel()
	src := "package p\nfunc f(bad bool) int {\nch := make(chan int)\nfor i := 0; i < 3; i++ {\ngo func() {\nch <- i\n}()\n}\nif bad {\nreturn -1\n}\nselect {\ncase v := <-ch:\nreturn v\ndefault:\nreturn -2\n}\n}\n"
	_, info, decls := testCheck(t, src)
	fn := decls[0]
	k := testVar(t, info, fn, "ch")
	loops := countLoops(fn)
	if len(loops) != 1 {
		t.Fatalf("want 1 loop, got %d", len(loops))
	}
	if got := straightRecvs(info, fn, loops[0], k); got != 0 {
		t.Errorf("select receive counted as straight: %d", got)
	}
	if got := directGos(loopBody(loops[0])); len(got) != 1 {
		t.Errorf("want 1 direct go, got %d", len(got))
	}
	if sliceIndex([]ast.Stmt{loops[0]}, nil) != -1 {
		t.Error("phantom index")
	}
	msgs := runCheck(t, src, firstresultCheck)
	if len(msgs) != 1 {
		t.Errorf("want 1 firstresult finding, got %v", msgs)
	}
}

func TestFirstresultReported(t *testing.T) {
	t.Parallel()
	src := "package p\nfunc f() int {\nch := make(chan int)\nfor i := 0; i < 3; i++ {\ngo func() {\nch <- i\n}()\n}\nreturn <-ch\n}\n"
	msgs := runCheck(t, src, firstresultCheck)
	if len(msgs) != 1 || !hasSub(msgs, "3 goroutines send on ch") {
		t.Errorf("want firstresult finding, got %v", msgs)
	}
}
