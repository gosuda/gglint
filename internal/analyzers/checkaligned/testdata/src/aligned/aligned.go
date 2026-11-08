package aligned

import (
	"sync/atomic"
	renamed "sync/atomic"
	"unsafe"
)

type counters struct {
	hits  int64
	flags uint32
	typed atomic.Int64
	ptr   unsafe.Pointer
	addr  uintptr
}

func positive(c *counters) {
	atomic.AddInt64(&c.hits, 1) // want `don't call atomic\.AddInt64; use the atomic\.Int64 type instead`

	_ = atomic.LoadUint32(&c.flags) // want `don't call atomic\.LoadUint32; use the atomic\.Uint32 type instead`

	_ = atomic.CompareAndSwapInt64(&c.hits, 1, 2) // want `don't call atomic\.CompareAndSwapInt64; use the atomic\.Int64 type instead`

	renamed.StoreUint32(&c.flags, 3) // want `don't call atomic\.StoreUint32; use the atomic\.Uint32 type instead`

	atomic.AndInt64(&c.hits, 3) // want `don't call atomic\.AndInt64; use the atomic\.Int64 type instead`

	atomic.OrUint32(&c.flags, 1) // want `don't call atomic\.OrUint32; use the atomic\.Uint32 type instead`
}

func functionValue() func(*uint64, uint64) uint64 {
	return atomic.SwapUint64 // want `don't call atomic\.SwapUint64; use the atomic\.Uint64 type instead`
}

func negative(c *counters) {
	c.typed.Add(1)
	_ = c.typed.Load()
	_ = atomic.LoadPointer(&c.ptr)
	atomic.AddUintptr(&c.addr, 1)
}

type fake struct {
	AddInt64 func(*int64, int64) int64
}

func add(p *int64, d int64) int64 {
	*p += d
	return *p
}

func shadowed(n *int64) {
	atomic := fake{AddInt64: add}
	atomic.AddInt64(n, 1)
}
