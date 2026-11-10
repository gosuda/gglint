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

func useafterputSilentGoAddr() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	go reopenPut(&b)
	_ = b.n
}

func useafterputSilentRangeAssign(ch chan *resBuf) {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	for b = range ch {
		_ = b.n
	}
}

func useafterputSilentFieldObj() int {
	type holder struct {
		b []byte
	}
	x := holder{b: make([]byte, 4)}
	y := holder{b: make([]byte, 4)}
	resPool.Put(x.b)
	return len(y.b)
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

func useafterputReportedReassign() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	b = b // want "was returned to the pool"
	_ = b
}

func useafterputSilentReassignGet() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	b = resPool.Get().(*resBuf)
	_ = b.n
}

func useResBuf(*resBuf) {}

func useafterputReportedCallArg() {
	b := resPool.Get().(*resBuf)
	resPool.Put(b)
	useResBuf(b) // want "was returned to the pool"
}
