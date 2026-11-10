package skipadvance

import "time"

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
			continue // want `this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceReportedIndex(a []int, n int) {
	i := 0
	for a[i] < n {
		if err := doStep(a[i]); err != nil {
			continue // want `this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue`
		}
		a[i]++
	}
}

func skipadvanceReportedStar(p *int, n int) {
	for *p < n {
		if err := doStep(*p); err != nil {
			continue // want `this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue`
		}
		(*p)++
	}
}

func skipadvanceSilentCustomErr(e *ptrErr) {
	i := 0
	for i < len(xs) {
		if e != nil {
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

func skipadvanceSilentNonErrorPtr(p *int) {
	i := 0
	for i < len(xs) {
		if p != nil {
			continue
		}
		i++
	}
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
			continue Outer // want `this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue`
		}
		i++
	}
}

func skipadvanceSilentAdvanceBefore() {
	i := 0
	for i < len(xs) {
		i++
		if err := doStep(xs[i]); err != nil {
			continue
		}
	}
}

func skipadvanceSilentInitAssign() {
	i := 0
	for i < len(xs) {
		if i = next(); stepErr != nil {
			continue
		}
		i++
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

func skipadvanceSilentNoAdvance() {
	i := 0
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			continue
		}
	}
}

func skipadvanceReportedRangeAdvance() {
	i := 0
	m := []int{1, 2, 3}
	for i < len(xs) {
		if err := doStep(xs[i]); err != nil {
			continue // want `this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue`
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
			continue // want `this continue skips the statement that advances the loop, so the same item is retried forever: advance before continue`
		}
		i++
	}
}
