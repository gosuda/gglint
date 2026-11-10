package subtest

import "testing"

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
		arr[0] = 2
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
