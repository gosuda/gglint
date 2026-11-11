package chanstate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/chanstate"
)

type modState uint8

const (
	modNil modState = iota
	modOpen
	modClosed
	modUnknown
)

func modStep(op string, cur modState) modState {
	if op == "make" {
		return modOpen
	}
	if op == "close" {
		return modClosed
	}
	if op == "escape" {
		return modUnknown
	}
	return cur
}

func modHasMake(seq []string) bool {
	return slices.Contains(seq, "make")
}

func modHasEscape(seq []string) bool {
	return slices.Contains(seq, "escape")
}

func modDouble(op string, pre modState) bool {
	return op == "close" && pre == modClosed
}

func modSend(op string, pre modState) bool {
	return op == "send" && pre == modClosed
}

func modNilRep(op string, pre modState, seq []string) bool {
	if modHasMake(seq) || modHasEscape(seq) {
		return false
	}
	if op != "close" && op != "send" && op != "recv" {
		return false
	}
	return pre == modNil
}

type modelGot struct {
	d map[int]map[int]bool
	s map[int]map[int]bool
	n map[int]map[int]bool
}

type chanProg interface {
	progName() string
	progSeq() []string
	progPre(int) modState
}

type plainProg struct {
	name string
	seq  []string
}

func (p plainProg) progName() string {
	return p.name
}

func (p plainProg) progSeq() []string {
	return p.seq
}

func (p plainProg) progPre(t int) modState {
	return foldOps(p.seq[:t])
}

type branchProg struct {
	name string
	seq  []string
	i    int
	j    int
}

func (p branchProg) progName() string {
	return p.name
}

func (p branchProg) progSeq() []string {
	return p.seq
}

func (p branchProg) progPre(t int) modState {
	return foldOps(branchPath(p.seq, p.i, p.j, t))
}

func foldOps(ops []string) modState {
	cur := modNil
	for _, op := range ops {
		cur = modStep(op, cur)
	}
	return cur
}

func branchPath(seq []string, i, j, target int) []string {
	if target < i {
		return slices.Clone(seq[:target])
	}
	if target < j {
		out := slices.Clone(seq[:i])
		return append(out, seq[i:target]...)
	}
	out := slices.Clone(seq[:i])
	return append(out, seq[j:target]...)
}

func chanAlphabet() []string {
	return []string{"make", "close", "send", "recv", "escape"}
}

func decodeSeq(ops []string, n, v int) []string {
	s := make([]string, n)
	for k := n - 1; k >= 0; k-- {
		s[k] = ops[v%len(ops)]
		v /= len(ops)
	}
	return s
}

func seqsOfLen(ops []string, n int) [][]string {
	total := 1
	for range n {
		total *= len(ops)
	}
	out := make([][]string, 0, total)
	for v := range total {
		out = append(out, decodeSeq(ops, n, v))
	}
	return out
}

func allSeqs(ops []string) [][]string {
	var out [][]string
	for n := 1; n <= 4; n++ {
		out = append(out, seqsOfLen(ops, n)...)
	}
	return out
}

func branchVariants(seq []string, base int) []chanProg {
	var out []chanProg
	for i := range len(seq) + 1 {
		for j := i; j <= len(seq); j++ {
			out = append(out, branchProg{name: fmt.Sprintf("f%d", base+len(out)), seq: seq, i: i, j: j})
		}
	}
	return out
}

func buildProgs() []chanProg {
	var out []chanProg
	for _, seq := range allSeqs(chanAlphabet()) {
		out = append(out, plainProg{name: fmt.Sprintf("f%d", len(out)), seq: seq})
		out = append(out, branchVariants(seq, len(out))...)
	}
	return out
}

func opText(op string) string {
	switch op {
	case "make":
		return "ch = make(chan int, 1)"
	case "close":
		return "close(ch)"
	case "send":
		return "ch <- 1"
	case "recv":
		return "<-ch"
	}
	return "_ = &ch"
}

type modelRender struct {
	lines []string
	lp    map[int]int
	ls    map[int]int
}

func (r *modelRender) emit(text string) {
	r.lines = append(r.lines, text)
}

func (r *modelRender) emitOp(pi, k int, op string) {
	n := len(r.lines) + 1
	r.lines = append(r.lines, opText(op))
	r.lp[n] = pi
	r.ls[n] = k
}

func renderPlain(r *modelRender, pi int, p plainProg) {
	r.emit("func " + p.name + "() {")
	r.emit("var ch chan int")
	for k := range p.seq {
		r.emitOp(pi, k, p.seq[k])
	}
	r.emit("_ = ch")
	r.emit("}")
	r.emit("")
}

func renderBranch(r *modelRender, pi int, p branchProg) {
	r.emit("func " + p.name + "(c bool) {")
	r.emit("var ch chan int")
	for k := range p.i {
		r.emitOp(pi, k, p.seq[k])
	}
	r.emit("if c {")
	for k := p.i; k < p.j; k++ {
		r.emitOp(pi, k, p.seq[k])
	}
	r.emit("} else {")
	for k := p.j; k < len(p.seq); k++ {
		r.emitOp(pi, k, p.seq[k])
	}
	r.emit("}")
	r.emit("_ = ch")
	r.emit("}")
	r.emit("")
}

func renderProgs(progs []chanProg) (src string, lp, ls map[int]int) {
	r := &modelRender{lines: []string{"package p", ""}, lp: map[int]int{}, ls: map[int]int{}}
	for pi, p := range progs {
		switch p := p.(type) {
		case plainProg:
			renderPlain(r, pi, p)
		case branchProg:
			renderBranch(r, pi, p)
		}
	}
	return strings.Join(r.lines, "\n"), r.lp, r.ls
}

func expectDouble(p chanProg, k int) bool {
	seq := p.progSeq()
	if modHasEscape(seq) {
		return false
	}
	return modDouble(seq[k], p.progPre(k))
}

func expectSend(p chanProg, k int) bool {
	seq := p.progSeq()
	if modHasEscape(seq) {
		return false
	}
	return modSend(seq[k], p.progPre(k))
}

func expectNil(p chanProg, k int) bool {
	seq := p.progSeq()
	return modNilRep(seq[k], p.progPre(k), seq)
}

func loadModelPkg(t *testing.T, src string) *packages.Package {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module modelp\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &packages.Config{
		Dir: dir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
			packages.NeedTypesSizes | packages.NeedModule,
		Env: append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("load: %v", pkgs[0].Errors)
	}
	return pkgs[0]
}

func recordModelDiag(got *modelGot, other *[]string, name string, pi, si int) {
	switch name {
	case "doubleclose":
		if got.d[pi] == nil {
			got.d[pi] = map[int]bool{}
		}
		got.d[pi][si] = true
	case "sendafterclose":
		if got.s[pi] == nil {
			got.s[pi] = map[int]bool{}
		}
		got.s[pi][si] = true
	case "nilop":
		if got.n[pi] == nil {
			got.n[pi] = map[int]bool{}
		}
		got.n[pi][si] = true
	default:
		*other = append(*other, name)
	}
}

func actualModelSets(pkg *packages.Package, lp, ls map[int]int) (got modelGot, other []string) {
	got = modelGot{d: map[int]map[int]bool{}, s: map[int]map[int]bool{}, n: map[int]map[int]bool{}}
	other = nil
	graph, err := checker.Analyze(chanstate.Analyzers(), []*packages.Package{pkg}, nil)
	if err != nil {
		return got, []string{err.Error()}
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			other = append(other, root.Analyzer.Name+":err")
		}
		for _, d := range root.Diagnostics {
			line := pkg.Fset.Position(d.Pos).Line
			pi, ok := lp[line]
			if !ok {
				other = append(other, root.Analyzer.Name+":offmap")
				continue
			}
			recordModelDiag(&got, &other, root.Analyzer.Name, pi, ls[line])
		}
	}
	return got, other
}

func checkModelProg(p chanProg, pi int, got modelGot) []string {
	var bad []string
	seq := p.progSeq()
	for k := range seq {
		if expectDouble(p, k) != got.d[pi][k] {
			bad = append(bad, fmt.Sprintf("%s seq=%v k=%d op=%s double want=%v got=%v", p.progName(), seq, k, seq[k], expectDouble(p, k), got.d[pi][k]))
		}
		if expectSend(p, k) != got.s[pi][k] {
			bad = append(bad, fmt.Sprintf("%s seq=%v k=%d op=%s send want=%v got=%v", p.progName(), seq, k, seq[k], expectSend(p, k), got.s[pi][k]))
		}
		if expectNil(p, k) != got.n[pi][k] {
			bad = append(bad, fmt.Sprintf("%s seq=%v k=%d op=%s nil want=%v got=%v", p.progName(), seq, k, seq[k], expectNil(p, k), got.n[pi][k]))
		}
		if len(bad) > 5 {
			break
		}
	}
	return bad
}

func TestModelEquivalence(t *testing.T) {
	t.Parallel()
	progs := buildProgs()
	t.Logf("enumerated %d functions", len(progs))
	src, lp, ls := renderProgs(progs)
	pkg := loadModelPkg(t, src)
	got, other := actualModelSets(pkg, lp, ls)
	if len(other) > 0 {
		t.Fatalf("unexpected diagnostics: %v", other[:min(10, len(other))])
	}
	var bad []string
	for pi, p := range progs {
		bad = append(bad, checkModelProg(p, pi, got)...)
		if len(bad) > 20 {
			break
		}
	}
	if len(bad) > 0 {
		t.Fatalf("model mismatch: %v", bad[:min(20, len(bad))])
	}
}
