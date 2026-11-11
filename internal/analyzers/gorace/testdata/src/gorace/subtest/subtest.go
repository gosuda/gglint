package subtest

import (
	"sync"
	"testing"
)

func subtestReported(t *testing.T) {
	var got int
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		got = 1
	})
	t.Run("b", func(t *testing.T) { // want `parallel subtests write got without ordering, so the test result depends on scheduling: give each subtest its own variable`
		t.Parallel()
		got = 2
	})
	_ = got
}

func subtestSilent(t *testing.T) {
	var got int
	t.Run("a", func(t *testing.T) {
		got = 1
	})
	t.Run("b", func(t *testing.T) {
		got = 2
	})
	_ = got
}

func subtestIndexSilent(t *testing.T) {
	var arr [2]int
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		arr[0] = 1
	})
	t.Run("b", func(t *testing.T) {
		t.Parallel()
		arr[1] = 2
	})
	_ = arr
}

func subtestDiffSilent(t *testing.T) {
	var a, b int
	t.Log("hi")
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		a = 1
	})
	t.Run("b", func(t *testing.T) {
		t.Parallel()
		b = 2
	})
	_, _ = a, b
}

func subtestCaseReported(t *testing.T, n int) {
	var got int
	switch n {
	case 1:
		t.Run("a", func(t *testing.T) {
			t.Parallel()
			got = 1
		})
		t.Run("b", func(t *testing.T) { // want `parallel subtests write got without ordering, so the test result depends on scheduling: give each subtest its own variable`
			t.Parallel()
			got = 2
		})
	}
	_ = got
}

func subtestFieldReported(t *testing.T) {
	var st struct{ n int }
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		st.n = 1
	})
	t.Run("b", func(t *testing.T) { // want `parallel subtests write st.n without ordering, so the test result depends on scheduling: give each subtest its own variable`
		t.Parallel()
		use(st)
	})
	_ = st
}

func use(any) {}

func subtestAssignedReported(t *testing.T) {
	var got int
	ok := t.Run("a", func(t *testing.T) {
		t.Parallel()
		got = 1
	})
	t.Run("b", func(t *testing.T) { // want `parallel subtests write got without ordering, so the test result depends on scheduling: give each subtest its own variable`
		t.Parallel()
		got = 2
	})
	_, _ = got, ok
}

func TestSubtestNested(t *testing.T) {
	t.Run("outer", func(t *testing.T) {
		var got int
		t.Run("a", func(t *testing.T) {
			t.Parallel()
			got = 1
		})
		t.Run("b", func(t *testing.T) { // want `parallel subtests write got without ordering, so the test result depends on scheduling: give each subtest its own variable`
			t.Parallel()
			got = 2
		})
		_ = got
	})
}

func subtestLockedSilent(t *testing.T) {
	var mu sync.Mutex
	n := 0
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		mu.Lock()
		n++
		mu.Unlock()
	})
	t.Run("b", func(t *testing.T) {
		t.Parallel()
		mu.Lock()
		n++
		mu.Unlock()
	})
	_ = n
}

func subtestLockedDeferSilent(t *testing.T) {
	var mu sync.Mutex
	n := 0
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		mu.Lock()
		defer mu.Unlock()
		n++
	})
	t.Run("b", func(t *testing.T) {
		t.Parallel()
		mu.Lock()
		defer mu.Unlock()
		n++
	})
	_ = n
}

func subtestOneSideReported(t *testing.T) {
	var mu sync.Mutex
	n := 0
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		mu.Lock()
		n++
		mu.Unlock()
	})
	t.Run("b", func(t *testing.T) { // want `parallel subtests write n without ordering, so the test result depends on scheduling: give each subtest its own variable`
		t.Parallel()
		n++
	})
	_ = n
}

func subtestDiffMuReported(t *testing.T) {
	var mu1, mu2 sync.Mutex
	n := 0
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		mu1.Lock()
		n++
		mu1.Unlock()
	})
	t.Run("b", func(t *testing.T) { // want `parallel subtests write n without ordering, so the test result depends on scheduling: give each subtest its own variable`
		t.Parallel()
		mu2.Lock()
		n++
		mu2.Unlock()
	})
	_ = n
}

func subtestPartialGuardReported(t *testing.T) {
	var mu sync.Mutex
	n := 0
	t.Run("a", func(t *testing.T) {
		t.Parallel()
		mu.Lock()
		n++
		mu.Unlock()
	})
	t.Run("b", func(t *testing.T) { // want `parallel subtests write n without ordering, so the test result depends on scheduling: give each subtest its own variable`
		t.Parallel()
		mu.Lock()
		n++
		mu.Unlock()
		n++
	})
	_ = n
}
