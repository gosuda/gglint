package useafterput

import (
	"sync"
)

type resBuf struct {
	n int
}

var resPool sync.Pool

func useafterputReported() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	b.n = 0 // want "was returned to the pool"
}

func useafterputSilent() {
	b := resPool.Get().(*resBuf)
	b.n = 0
	resPool.Put(b)
}

func useafterputSilentInc() {
	b := resPool.Get().(*resBuf)
	n := 0
	b.n = 0
	resPool.Put(b)
	n++
	_ = n
}

type putHolder struct {
	data []byte
	n    int
}

func useafterputSilentFieldArg() {
	b := &putHolder{}
	resPool.Put(b.data)
	b.n = 0
}

func reopenPut(p **resBuf) {
	*p = &resBuf{}
}

func useafterputSilentAddr() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	reopenPut(&b)
	_ = b.n
}

func useafterputSilentRangeAssign(ch chan *resBuf) {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	for b = range ch {
		_ = b.n
	}
}


func useafterputReportedLoop() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	for range 2 {
		_ = b.n // want "was returned to the pool"
	}
}

func useafterputReportedAfterCopy() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	n := b.n // want "was returned to the pool"
	_ = b.n  // want "was returned to the pool"
	_ = n
}

func useafterputSilentSelfAssign() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	b = b
	_ = b
}

func useafterputSilentReassignGet() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	b = resPool.Get().(*resBuf)
	_ = b.n
}

func useResBuf(b *resBuf) {
	_ = b.n
}

func useafterputReportedCallArg() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	useResBuf(b) // want "was returned to the pool"
}

func usePutBytes(p []byte) {
	_ = p
}

func useafterputReportedSliceCall() {
	b := resPool.Get().([]byte)
	resPool.Put(b)
	usePutBytes(b[:]) // want "was returned to the pool"
}

func useafterputReportedSliceStore() {
	b := resPool.Get().([]byte)
	resPool.Put(b)
	x := b[:] // want "was returned to the pool"
	_ = x
}

func useafterputSilentSelfSlice() {
	b := resPool.Get().([]byte)
	resPool.Put(b)
	b = b[:0]
	_ = b
}
func useafterputReportedSelfSliceAppend() {
	b := resPool.Get().([]byte)
	resPool.Put(b)
	b = b[:0]
	b = append(b, 1) // want "was returned to the pool"
	_ = b
}

func useafterputReportedVarDecl() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	var z int
	_ = z
	_ = b.n // want "was returned to the pool"
}

func useafterputSilentRefillInPlace() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	tries := 0
	func() {
		b = resPool.Get().(*resBuf)
		tries = 0
	}()
	_ = b.n
	_ = tries
}

func useafterputSilentValueCopy() {
	v := struct{ N int }{}
	resPool.Put(v)
	v.N++
	_ = v.N
}
