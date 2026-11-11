package resstate_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/resstate"
)

type modelProg struct {
	seq      []int
	branched bool
	i        int
	j        int
	lines    []int
}

func modelStmt(op int) string {
	switch op {
	case 0:
		return "f.Close()"
	case 1:
		return "f.Read(nil)"
	case 2:
		return "f = R{}"
	default:
		return "sink(f)"
	}
}

func modelNext(op, st int) int {
	if op == 0 {
		return 1
	}
	if op == 2 {
		return 2
	}
	if op == 3 {
		return 2
	}
	return st
}

func modelPlain(seq []int) map[int]bool {
	out := map[int]bool{}
	st := 0
	for idx, op := range seq {
		if op == 1 && st == 1 {
			out[idx] = true
		}
		st = modelNext(op, st)
	}
	return out
}

func modelPrefix(seq []int, end int) int {
	st := 0
	for k := range end {
		st = modelNext(seq[k], st)
	}
	return st
}

func modelRange(seq []int, from, start, end int, out map[int]bool) {
	st := from
	for k := start; k < end; k++ {
		if seq[k] == 1 && st == 1 {
			out[k] = true
		}
		st = modelNext(seq[k], st)
	}
}

func modelBranch(seq []int, i, j int) map[int]bool {
	out := map[int]bool{}
	pre := modelPrefix(seq, i)
	for k := range i {
		if seq[k] == 1 && modelPrefix(seq, k) == 1 {
			out[k] = true
		}
	}
	modelRange(seq, pre, i, j, out)
	modelRange(seq, pre, j, len(seq), out)
	return out
}

func modelExpected(p modelProg) map[int]bool {
	if p.branched {
		return modelBranch(p.seq, p.i, p.j)
	}
	return modelPlain(p.seq)
}

func modelDecode(code, n int) []int {
	seq := make([]int, n)
	for k := range n {
		seq[k] = code % 4
		code /= 4
	}
	return seq
}

func modelPow(n int) int {
	p := 1
	for range n {
		p *= 4
	}
	return p
}

func modelAppendPlain(lines []string, seq []int, id int) ([]string, modelProg) {
	prog := modelProg{seq: slices.Clone(seq)}
	lines = append(lines, "func f"+strconv.Itoa(id)+"(c bool) {", "f := R{}")
	prog.lines = make([]int, len(seq))
	for k, op := range seq {
		lines = append(lines, modelStmt(op))
		prog.lines[k] = len(lines)
	}
	lines = append(lines, "_ = f", "}")
	return lines, prog
}

func modelAppendBranch(lines []string, seq []int, i, j, id int) ([]string, modelProg) {
	prog := modelProg{seq: slices.Clone(seq), branched: true, i: i, j: j}
	prog.lines = make([]int, len(seq))
	lines = append(lines, "func f"+strconv.Itoa(id)+"(c bool) {", "f := R{}")
	for k := range i {
		lines = append(lines, modelStmt(seq[k]))
		prog.lines[k] = len(lines)
	}
	lines = append(lines, "if c {")
	for k := i; k < j; k++ {
		lines = append(lines, modelStmt(seq[k]))
		prog.lines[k] = len(lines)
	}
	lines = append(lines, "} else {")
	for k := j; k < len(seq); k++ {
		lines = append(lines, modelStmt(seq[k]))
		prog.lines[k] = len(lines)
	}
	lines = append(lines, "}", "_ = f", "}")
	return lines, prog
}

func modelSplits(lines []string, progs []modelProg, seq []int, id int) (nextLines []string, nextProgs []modelProg, nextID int) {
	n := len(seq)
	for i := range n + 1 {
		for j := i; j <= n; j++ {
			var p modelProg
			lines, p = modelAppendBranch(lines, seq, i, j, id)
			progs = append(progs, p)
			id++
		}
	}
	return lines, progs, id
}

func modelGen() ([]string, []modelProg) {
	lines := []string{"package p", "", "import \"os\"", "", "type R struct { *os.File }", "", "func sink(any) {}", ""}
	progs := []modelProg{}
	id := 0
	for n := range 5 {
		total := modelPow(n)
		for code := range total {
			seq := modelDecode(code, n)
			var p modelProg
			lines, p = modelAppendPlain(lines, seq, id)
			progs = append(progs, p)
			id++
			lines, progs, id = modelSplits(lines, progs, seq, id)
		}
	}
	return lines, progs
}

func modelWrite(t *testing.T, dir, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module modelp\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("write p.go: %v", err)
	}
}

func modelLoad(t *testing.T, dir string) []*packages.Package {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.LoadAllSyntax,
		Dir:  dir,
		Env: append(os.Environ(),
			"GO111MODULE=on",
			"GOFLAGS=-mod=mod",
			"GOPROXY=off",
			"GOTOOLCHAIN=local",
			"GOWORK=off",
		),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("want one package, got %d", len(pkgs))
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatalf("package has errors")
	}
	return pkgs
}

func modelActual(t *testing.T, pkgs []*packages.Package, progs []modelProg) []map[int]bool {
	t.Helper()
	graph, err := checker.Analyze([]*analysis.Analyzer{resstate.Analyzers()[0]}, pkgs, nil)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
		}
	}
	lineMap := map[int]int{}
	progOf := map[int]int{}
	for pi, p := range progs {
		for si, ln := range p.lines {
			lineMap[ln] = si
			progOf[ln] = pi
		}
	}
	actual := make([]map[int]bool, len(progs))
	for i := range actual {
		actual[i] = map[int]bool{}
	}
	for _, root := range graph.Roots {
		for _, d := range root.Diagnostics {
			pos := pkgs[0].Fset.Position(d.Pos)
			ln := pos.Line
			pi, ok := progOf[ln]
			if !ok {
				t.Fatalf("diagnostic at unexpected line %d: %s", ln, d.Message)
			}
			actual[pi][lineMap[ln]] = true
		}
	}
	return actual
}

func modelCompare(t *testing.T, progs []modelProg, actual []map[int]bool) {
	t.Helper()
	bad := 0
	for i, p := range progs {
		want := modelExpected(p)
		got := actual[i]
		if len(want) != len(got) {
			modelFail(t, i, p, want, got, &bad)
			continue
		}
		for k := range want {
			if !got[k] {
				modelFail(t, i, p, want, got, &bad)
				break
			}
		}
		if bad >= 5 {
			t.Fatalf("too many mismatches")
		}
	}
	if bad > 0 {
		t.Fatalf("%d mismatches", bad)
	}
}

func modelFail(t *testing.T, i int, p modelProg, want, got map[int]bool, bad *int) {
	t.Helper()
	*bad++
	var b strings.Builder
	b.WriteString("prog ")
	b.WriteString(strconv.Itoa(i))
	b.WriteString(" seq=")
	for _, op := range p.seq {
		b.WriteString(strconv.Itoa(op))
	}
	t.Logf("%s branched=%v i=%d j=%d want=%v got=%v", b.String(), p.branched, p.i, p.j, want, got)
}

func TestModelEquivalence(t *testing.T) {
	t.Parallel()
	lines, progs := modelGen()
	t.Logf("functions=%d", len(progs))
	dir := t.TempDir()
	modelWrite(t, dir, strings.Join(lines, "\n")+"\n")
	pkgs := modelLoad(t, dir)
	actual := modelActual(t, pkgs, progs)
	modelCompare(t, progs, actual)
}
