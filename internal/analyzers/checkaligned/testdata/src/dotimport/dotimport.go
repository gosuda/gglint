package dotimport

import . "sync/atomic"

func increment(n *int32) int32 {
	return AddInt32(n, 1) // want `don't call atomic\.AddInt32; use the atomic\.Int32 type instead`
}

func pointerSwap(v *Value) any {
	return v.Swap(1)
}
