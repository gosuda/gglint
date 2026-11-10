package lockstate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/lockstate"
)

type modKind uint8

const (
	modFree modKind = iota
	modWrite
	modRead
	modUnknown
)

type modState struct {
	kind  modKind
	depth int
	exact bool
}

func modInexactRead(depth int) modState {
	return modState{kind: modRead, depth: depth}
}

func modStep(op string, cur modState) modState {
	switch op {
	case "Lock":
		return modState{kind: modWrite}
	case "RLock":
		return modRLock(cur)
	case "Unlock":
		if cur.kind == modWrite {
			return modState{kind: modFree}
		}
		return modState{kind: modUnknown}
	case "RUnlock":
		return modRUnlock(cur)
	}
	return modState{kind: modUnknown}
}

func modRLock(cur modState) modState {
	if cur.kind == modWrite {
		return modState{kind: modUnknown}
	}
	if cur.kind == modRead {
		if !cur.exact {
			return modInexactRead(cur.depth + 1)
		}
		return modState{kind: modRead, depth: cur.depth + 1, exact: true}
	}
	if cur.kind == modUnknown {
		return modInexactRead(1)
	}
	return modState{kind: modRead, depth: 1, exact: true}
}

func modRUnlock(cur modState) modState {
	if cur.kind == modRead && !cur.exact {
		if cur.depth >= 2 {
			return modInexactRead(cur.depth - 1)
		}
		return modState{kind: modUnknown}
	}
	if cur.kind == modRead && cur.depth > 1 {
		return modState{kind: modRead, depth: cur.depth - 1, exact: true}
	}
	if cur.kind == modRead {
		return modState{kind: modFree}
	}
	return modState{kind: modUnknown}
}

func modPairReport(op string, pre modState) bool {
	if op == "Unlock" {
		return pre.kind == modRead || pre.kind == modFree
	}
	if op == "RUnlock" {
		return pre.kind == modWrite || pre.kind == modFree
	}
	return false
}

func modUpgradeReport(op string, pre modState) bool {
	return op == "Lock" && pre.kind == modRead
}

type modelProg interface {
	progName() string
	progSeq() []string
	progPre(target int) modState
}

type plainProg struct {
	name   string
	muType string
	seq    []string
	param  bool
}

func (p plainProg) progName() string  { return p.name }
func (p plainProg) progSeq() []string { return p.seq }
func (p plainProg) progPre(t int) modState {
	if p.param {
		return foldParamOps(p.seq[:t])
	}
	return foldModelOps(p.seq[:t])
}

type branchProg struct {
	name   string
	muType string
	seq    []string
	i      int
	j      int
	param  bool
}

func (p branchProg) progName() string  { return p.name }
func (p branchProg) progSeq() []string { return p.seq }
func (p branchProg) progPre(target int) modState {
	ops := branchPathOps(p.seq, p.i, p.j, target)
	if p.param {
		return foldParamOps(ops)
	}
	return foldModelOps(ops)
}

func foldModelOps(ops []string) modState {
	return foldFrom(modState{kind: modFree}, ops)
}

func foldParamOps(ops []string) modState {
	return foldFrom(modState{kind: modUnknown}, ops)
}

func foldFrom(entry modState, ops []string) modState {
	cur := entry
	for _, op := range ops {
		cur = modStep(op, cur)
	}
	return cur
}

func branchPathOps(seq []string, i, j, target int) []string {
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

func rwOps() []string {
	return []string{"Lock", "Unlock", "RLock", "RUnlock"}
}

func mutexOps() []string {
	return []string{"Lock", "Unlock"}
}

func decodeModelSeq(ops []string, n, v int) []string {
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
		out = append(out, decodeModelSeq(ops, n, v))
	}
	return out
}

func allModelSeqs(ops []string) [][]string {
	var out [][]string
	for n := 1; n <= 4; n++ {
		out = append(out, seqsOfLen(ops, n)...)
	}
	return out
}

func branchLocalVariants(muType string, seq []string, base int) []modelProg {
	var out []modelProg
	for i := range len(seq) + 1 {
		for j := i; j <= len(seq); j++ {
			out = append(out, branchProg{name: fmt.Sprintf("f%d", base+len(out)), muType: muType, seq: seq, i: i, j: j})
		}
	}
	return out
}

func branchParamVariants(muType string, seq []string, base int) []modelProg {
	var out []modelProg
	for i := range len(seq) + 1 {
		for j := i; j <= len(seq); j++ {
			out = append(out, branchProg{name: fmt.Sprintf("f%d", base+len(out)), muType: muType, seq: seq, i: i, j: j, param: true})
		}
	}
	return out
}

func buildModelProgs() []modelProg {
	var out []modelProg
	for _, tc := range []struct {
		muType string
		ops    []string
	}{{"sync.RWMutex", rwOps()}, {"sync.Mutex", mutexOps()}} {
		for _, seq := range allModelSeqs(tc.ops) {
			out = append(out, plainProg{name: fmt.Sprintf("f%d", len(out)), muType: tc.muType, seq: seq})
			out = append(out, branchLocalVariants(tc.muType, seq, len(out))...)
			out = append(out, plainProg{name: fmt.Sprintf("f%d", len(out)), muType: tc.muType, seq: seq, param: true})
			out = append(out, branchParamVariants(tc.muType, seq, len(out))...)
		}
	}
	return out
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
	r.lines = append(r.lines, "mu."+op+"()")
	r.lp[n] = pi
	r.ls[n] = k
}

func renderPlainProg(r *modelRender, pi int, p plainProg) {
	if p.param {
		renderParamHead(r, p.name, p.muType)
	} else {
		r.emit("func " + p.name + "() {")
		r.emit("var mu " + p.muType)
	}
	for k := range p.seq {
		r.emitOp(pi, k, p.seq[k])
	}
	r.emit("}")
	r.emit("")
}

func renderBranchProg(r *modelRender, pi int, p branchProg) {
	if p.param {
		r.emit("func " + p.name + "(mu *" + p.muType + ", c bool) {")
	} else {
		r.emit("func " + p.name + "(c bool) {")
		r.emit("var mu " + p.muType)
	}
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
	r.emit("}")
	r.emit("")
}

func renderParamHead(r *modelRender, name, muType string) {
	r.emit("func " + name + "(mu *" + muType + ") {")
}

func renderModelProgs(progs []modelProg) (src string, lp, ls map[int]int) {
	r := &modelRender{lines: []string{"package p", "", `import "sync"`, ""}, lp: map[int]int{}, ls: map[int]int{}}
	for pi, p := range progs {
		switch p := p.(type) {
		case plainProg:
			renderPlainProg(r, pi, p)
		case branchProg:
			renderBranchProg(r, pi, p)
		}
	}
	return strings.Join(r.lines, "\n"), r.lp, r.ls
}

func expectModelPair(p modelProg, k int) bool {
	return modPairReport(p.progSeq()[k], p.progPre(k))
}

func expectModelUpgrade(p modelProg, k int) bool {
	return modUpgradeReport(p.progSeq()[k], p.progPre(k))
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

func recordModelDiag(pairGot, upGot map[int]map[int]bool, other *[]string, name string, pi, si int) {
	switch name {
	case "pair":
		if pairGot[pi] == nil {
			pairGot[pi] = map[int]bool{}
		}
		pairGot[pi][si] = true
	case "upgrade":
		if upGot[pi] == nil {
			upGot[pi] = map[int]bool{}
		}
		upGot[pi][si] = true
	default:
		*other = append(*other, name)
	}
}

func actualModelSets(pkg *packages.Package, lp, ls map[int]int) (pairGot, upGot map[int]map[int]bool, other []string) {
	pairGot = map[int]map[int]bool{}
	upGot = map[int]map[int]bool{}
	other = nil
	graph, err := checker.Analyze(lockstate.Analyzers(), []*packages.Package{pkg}, nil)
	if err != nil {
		return pairGot, upGot, []string{err.Error()}
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
			recordModelDiag(pairGot, upGot, &other, root.Analyzer.Name, pi, ls[line])
		}
	}
	return pairGot, upGot, other
}

func checkModelProg(p modelProg, pi int, pairGot, upGot map[int]map[int]bool) []string {
	var bad []string
	seq := p.progSeq()
	for k := range seq {
		if expectModelPair(p, k) != pairGot[pi][k] {
			bad = append(bad, fmt.Sprintf("%s seq=%v k=%d op=%s pair want=%v got=%v", p.progName(), seq, k, seq[k], expectModelPair(p, k), pairGot[pi][k]))
		}
		if expectModelUpgrade(p, k) != upGot[pi][k] {
			bad = append(bad, fmt.Sprintf("%s seq=%v k=%d op=%s upgrade want=%v got=%v", p.progName(), seq, k, seq[k], expectModelUpgrade(p, k), upGot[pi][k]))
		}
	}
	return bad
}

func TestModelEquivalence(t *testing.T) {
	t.Parallel()
	progs := buildModelProgs()
	t.Logf("enumerated %d functions", len(progs))
	src, lp, ls := renderModelProgs(progs)
	pkg := loadModelPkg(t, src)
	pairGot, upGot, other := actualModelSets(pkg, lp, ls)
	if len(other) > 0 {
		t.Fatalf("unexpected diagnostics: %v", other[:min(10, len(other))])
	}
	var bad []string
	for pi, p := range progs {
		bad = append(bad, checkModelProg(p, pi, pairGot, upGot)...)
		if len(bad) > 20 {
			break
		}
	}
	if len(bad) > 0 {
		t.Fatalf("model mismatch: %v", bad[:min(20, len(bad))])
	}
}
