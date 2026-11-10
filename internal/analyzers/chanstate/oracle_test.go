package chanstate_test

import (
	"fmt"
	"testing"
)

func oracleProgs() []chanProg {
	var out []chanProg
	for _, seq := range allSeqs(chanAlphabet()) {
		if seq[0] != "make" || modHasEscape(seq) {
			continue
		}
		out = append(out, plainProg{name: fmt.Sprintf("f%d", len(out)), seq: seq})
	}
	return out
}

func oracleStep(op string, ch chan int) (out chan int, panicked bool) {
	out = ch
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	switch op {
	case "make":
		out = make(chan int, 1)
	case "close":
		close(ch)
	case "send":
		select {
		case ch <- 1:
		default:
		}
	case "recv":
		select {
		case <-ch:
		default:
		}
	}
	return out, false
}

func oracleRun(seq []string) []bool {
	ch := make(chan int, 1)
	out := make([]bool, len(seq))
	for k, op := range seq {
		var panicked bool
		ch, panicked = oracleStep(op, ch)
		out[k] = panicked
	}
	return out
}

func checkOracleProg(p chanProg, pi int, got modelGot) []string {
	var bad []string
	seq := p.progSeq()
	panics := oracleRun(seq)
	for k := range seq {
		reported := got.d[pi][k] || got.s[pi][k]
		if reported != panics[k] {
			bad = append(bad, fmt.Sprintf("%s seq=%v k=%d op=%s reported=%v panicked=%v", p.progName(), seq, k, seq[k], reported, panics[k]))
		}
		if len(bad) > 5 {
			break
		}
	}
	return bad
}

func TestRuntimeOracle(t *testing.T) {
	t.Parallel()
	progs := oracleProgs()
	t.Logf("oracle enumerated %d functions", len(progs))
	src, lp, ls := renderProgs(progs)
	pkg := loadModelPkg(t, src)
	got, other := actualModelSets(pkg, lp, ls)
	if len(other) > 0 {
		t.Fatalf("unexpected diagnostics: %v", other[:min(10, len(other))])
	}
	var bad []string
	for pi, p := range progs {
		bad = append(bad, checkOracleProg(p, pi, got)...)
		if len(bad) > 20 {
			break
		}
	}
	if len(bad) > 0 {
		t.Fatalf("oracle mismatch: %v", bad[:min(20, len(bad))])
	}
}
