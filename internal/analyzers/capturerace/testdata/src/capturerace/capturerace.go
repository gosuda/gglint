package capturerace

import (
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type pair struct {
	a, b int
}

type oneWord struct {
	n int
}

type twoHalves struct {
	a, b int32
}

type padded struct {
	a byte
	b int32
	c byte
}

type zeroTail struct {
	n int
	z struct{}
}

type named struct {
	name string
}

type holder struct {
	p *named
	n int
}

type table struct {
	m    map[string]int
	name string
}

var global string

func use(s string) { println(s) }

func call(f func()) { f() }

func stringParentWrites() {
	s := "a"
	go func() { // want `s is written after a goroutine that captures it starts: pass it as an argument or synchronize first`
		println(s)
	}()
	s = "b"
}

func stringChildWrites() {
	s := "a"
	go func() { // want `s is written by a goroutine and read here without synchronization: wait for the goroutine first`
		s = "b"
	}()
	println(s)
}

func childWritesWithoutLaterRead() {
	s := "a"
	println(s)
	go func() {
		s = "b"
	}()
}

func sliceHeader() {
	xs := []int{1}
	go func() { // want `xs is written after a goroutine that captures it starts`
		println(len(xs))
	}()
	xs = append(xs, 2)
}

func interfaceValue() {
	var v any = 1
	go func() { // want `v is written after a goroutine that captures it starts`
		println(v != nil)
	}()
	v = "x"
}

func complex128Value() {
	c := complex(1, 2)
	go func() { // want `c is written by a goroutine and read here`
		c = complex(3, 4)
	}()
	println(real(c))
}

func complex64Value() {
	var c complex64
	go func() { // want `c is written after a goroutine that captures it starts`
		println(real(c))
	}()
	c = 1
}

func multiWordStructField() {
	var p pair
	go func() { // want `p is written after a goroutine that captures it starts`
		println(p.a)
	}()
	p.b = 1
}

func singleWordStruct() {
	var o oneWord
	go func() {
		println(o.n)
	}()
	o.n = 1
}

func twoHalvesStruct() {
	var h twoHalves
	go func() {
		println(h.a)
	}()
	h.b = 1
}

func paddingOnlyStruct() {
	var p padded
	go func() {
		println(p.a)
	}()
	p.c = 1
}

func zeroSizeTail() {
	var z zeroTail
	go func() {
		println(z.n)
	}()
	z.n = 1
}

func arrayElementByChild() {
	var a [2]int
	go func() { // want `a is written by a goroutine and read here`
		a[0] = 1
	}()
	println(a[1])
}

func oneWordArray() {
	var a [1]int
	go func() {
		a[0] = 1
	}()
	println(a[0])
}

func oneStringArray() {
	var a [1]string
	go func() { // want `a is written after a goroutine that captures it starts`
		println(a[0])
	}()
	a[0] = "x"
}

func syncTypesExempt() {
	var wg sync.WaitGroup
	var v atomic.Value
	wg.Add(1)
	go func() {
		defer wg.Done()
		v.Store("x")
	}()
	wg = sync.WaitGroup{}
	v = atomic.Value{}
	println(v.Load() != nil)
}

func sliceElementWrites() {
	xs := []int{1, 2}
	go func() {
		xs[0] = 1
	}()
	xs[1] = 2
}

func mapEntryWrites() {
	t := table{m: map[string]int{}}
	go func() {
		t.m["a"] = 1
	}()
	t.m["b"] = 2
	println(t.name)
}

func storeThroughPointerField() {
	h := holder{p: &named{}}
	go func() {
		h.p.name = "a"
	}()
	h.p.name = "b"
	println(h.n)
}

func storeThroughAddress() {
	s := "a"
	p := &s
	go func() {
		println(s)
	}()
	*p = "b"
}

func rangeAssign() {
	var s string
	go func() { // want `s is written after a goroutine that captures it starts`
		println(s)
	}()
	for _, s = range []string{"a"} {
	}
}

func compoundAssign() {
	s := "a"
	go func() { // want `s is written after a goroutine that captures it starts`
		println(s)
	}()
	s += "b"
}

func increment() {
	var p pair
	go func() { // want `p is written after a goroutine that captures it starts`
		println(p.a)
	}()
	p.a++
}

func redeclared() {
	s := "a"
	go func() { // want `s is written after a goroutine that captures it starts`
		println(s)
	}()
	s, n := "b", 1
	println(n)
}

func writtenBeforeSpawn() {
	s := "a"
	s = "b"
	go func() {
		println(s)
	}()
}

func passedAsArgument() {
	s := "a"
	go func(s string) {
		s = "c"
		println(s)
	}(s)
	s = "b"
}

func passedToFunction() {
	s := "a"
	go use(s)
	s = "b"
}

func packageVariable() {
	go func() {
		global = "b"
	}()
	global = "c"
}

func nestedLiteralWrites() {
	s := "a"
	go func() { // want `s is written after a goroutine that captures it starts`
		println(s)
	}()
	func() {
		s = "b"
	}()
}

func spawnInsideGoroutine() {
	s := "a"
	go func() { // want `s is written by a goroutine and read here`
		println("outer")
		go func() {
			s = "b"
		}()
	}()
	println(s)
}

func parenthesizedLiteral() {
	s := "a"
	go (func() { // want `s is written by a goroutine and read here`
		s = "b"
	})()
	println(s)
}

func typeParamValue[T any](v T) {
	go func() {
		println(&v)
	}()
	v = *new(T)
}

func errgroupGo() error {
	var g errgroup.Group
	s := "a"
	g.Go(func() error { // want `s is written after a goroutine that captures it starts`
		println(s)
		return nil
	})
	s = "b"
	return g.Wait()
}

func errgroupTryGo() {
	var g errgroup.Group
	xs := []string{"a"}
	g.TryGo(func() error { // want `xs is written by a goroutine and read here`
		xs = nil
		return nil
	})
	println(len(xs))
}

func waitGroupGoIgnoresWait() {
	var wg sync.WaitGroup
	s := "a"
	wg.Go(func() { // want `s is written by a goroutine and read here`
		s = "b"
	})
	wg.Wait()
	println(s)
}

func plainCallIsNoSpawn() {
	s := "a"
	call(func() {
		s = "b"
	})
	println(s)
}
