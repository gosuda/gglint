package skipadvance

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

var xs = []int{1, 2, 3}

var stepErr error

func doStep(int) error { return nil }

func next() int { return 0 }

type ptrErr struct{}

func (*ptrErr) Error() string { return "e" }

func skipadvanceReported() {
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceReportedIndex(a []int, n int) {
	i := 0
	for a[i] < n {
		if err := doStep(a[i]); err != nil {
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		a[i]++
	}
}

func skipadvanceReportedStar(p *int, n int) {
	for *p < n {
		if err := doStep(*p); err != nil {
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		(*p)++
	}
}

func skipadvanceSilentCustomErr(e *ptrErr) {
	i := 0
	for i < len(xs) {
		if e != nil {
			e = nil
			continue
		}
		i++
	}
}

func skipadvanceSilent() {
	for i := 0; i < len(xs); i++ {
		if err := doStep(xs[i]); err != nil {
			continue
		}
	}
}

func skipadvanceSilentNoError() {
	done := false
	i := 0
	for i < len(xs) {
		if done {
			continue
		}
		i++
	}
}

func skipadvanceSilentIntCompare() {
	x := 0
	i := 0
	for i < len(xs) {
		if x > 0 {
			continue
		}
		i++
	}
	_ = x
}

func skipadvanceSilentBreak() {
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			break
		}
		i++
	}
}

func skipadvanceReportedLabeled() {
	i := 0
Outer:
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			continue Outer // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceSilentAdvanceBefore() {
	i := 0
	for i < len(xs) {
		v := xs[i]
		i++
		if err := doStep(v); err != nil {
			continue
		}
	}
}

func skipadvanceSilentInitAssign() {
	i := 0
	for i < len(xs) {
		if i = i + 1; stepErr != nil {
			continue
		}
	}
}

func skipadvanceSilentInnerAssign() {
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			i++
			continue
		}
		i++
	}
}

func skipadvanceReportedRangeAdvance() {
	i := 0
	m := []int{1, 2, 3}
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		for _, i = range m {
		}
	}
}

func skipadvanceSilentOuterLabel() {
	i := 0
Outer:
	for j := 0; j < len(xs); j++ {
		for i < len(xs) {
			if err := doStep(xs[i]); err != nil {
				continue Outer
			}
			i++
		}
	}
}

func skipadvanceReportedSleep() {
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			time.Sleep(time.Second)
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceSilentCursorAdvance(words []string, n int) (out []int) {
	for len(out) < n {
		if len(words) == 0 {
			return out
		}
		w := words[0]
		words = words[1:]
		v, err := strconv.Atoi(w)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}
func skipadvanceReportedOuterWrite(xs []int, lastErr *error) {
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			*lastErr = err
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceReportedCounter(xs []int) {
	failures := 0
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			failures++
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
	_ = failures
}

func skipadvanceSilentAdvanceBeforeAndAfter() {
	i := 0
	for i < len(xs) {
		i++
		if stepErr != nil {
			continue
		}
		i++
	}
}

func skipadvanceSilentFreshChan(n int, errs <-chan error) int {
	i := 0
	for i < n {
		if err := <-errs; err != nil {
			continue
		}
		i++
	}
	return i
}

func skipadvanceReportedAttempts(xs []int) {
	i := 0
	attempts := 0
	for i < len(xs) {
		attempts++
		fmt.Println(attempts)
		if err := doStep(xs[i]); err != nil {
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}


var errTransient error = &ptrErr{}

var skipGate int

func stepGated(i int) error {
	if skipGate < 3 {
		return errTransient
	}
	return nil
}

func skipadvanceSilentGateFromCounter(n int) {
	i, w := 0, 0
	for i < n {
		skipGate = w
		if err := stepGated(i); err != nil {
			w++
			continue
		}
		i++
	}
}

func skipadvanceReportedCounterOnlyLogged(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if attempts > 3 {
			fmt.Println("many")
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceSilentBoundedRetry(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if attempts > 3 {
			break
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}

func skipadvanceSilentBoundedRetrySwitch(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		switch {
		case attempts > 3:
			return
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}

func giveUp() { panic("gave up") }

func skipadvanceSilentBoundedRetryHelperLeaves(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if attempts > 3 {
			giveUp()
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}

func skipadvanceSilentBoundedRetryElseLeaves(xs []int) {
	i, attempts, seen := 0, 0, 0
	for i < len(xs) {
		if attempts < 3 {
			seen++
		} else {
			return
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
	_ = seen
}

func skipadvanceSilentBoundedRetryHeaderCopy(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if n := attempts; n > 3 {
			return
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}

func skipadvanceReportedCounterOnlySlogged(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if attempts > 3 {
			slog.Info("many attempts", slog.Int("attempts", attempts))
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue // want `this continue skips the statement that advances the loop, so if the error repeats, the same item is retried forever: advance before continue`
		}
		i++
	}
}

func capAttempts(n int) int {
	if n > 3 {
		panic("too many")
	}
	return n
}

func skipadvanceSilentPrintWithCall(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if attempts > 3 {
			slog.Info("attempts", slog.Int("n", capAttempts(attempts)))
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}

func skipadvanceSilentPrintThenLeave(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if attempts > 3 {
			slog.Info("many")
			return
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}

func tooMany(n int) bool {
	if n > 3 {
		panic("too many")
	}
	return false
}

func skipadvanceSilentPrintCondCall(xs []int) {
	i, attempts := 0, 0
	for i < len(xs) {
		if tooMany(attempts) {
			slog.Info("many")
		}
		if err := doStep(xs[i]); err != nil {
			attempts++
			continue
		}
		i++
	}
}
