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
