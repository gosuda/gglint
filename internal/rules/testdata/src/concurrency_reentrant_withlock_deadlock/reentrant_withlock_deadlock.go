// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in third_party/logos/LICENSE.
// Adapted from loGos test/logos/lint/reentrant_withlock_deadlock_go.go.

package p

import "sync"

var global sync.Mutex

var other sync.Mutex

var byName sync.Mutex

type Counter struct {
	mu sync.Mutex
	n  int
}

type Wrapper struct {
	inner Counter
	mu    sync.Mutex
}

type Node struct {
	mu   sync.Mutex
	next *Node
}

func withLock(l *sync.Mutex, fn func()) {
	l.Lock()
	defer l.Unlock()
	fn()
}

func setOne(n *int) {
	*n = 1
}

func sink(v int) {}

func lockGlobal() {
	global.Lock()
	global.Unlock()
}

func lockGlobalTwice() {
	lockGlobal()
	lockGlobal()
}

func lockParam(l *sync.Mutex) {
	l.Lock()
	l.Unlock()
}

func lockParamTwoDeep(l *sync.Mutex) {
	lockParam(l)
}

func lockOther() {
	other.Lock()
	other.Unlock()
}

func lockOtherTwice() {
	lockOther()
	lockOther()
}

func lockAndReturn() int {
	lockGlobal()
	return 0
}

func chainOne(l *sync.Mutex) {
	lockParam(l)
}

func chainTwo(l *sync.Mutex) {
	chainOne(l)
}

func chainThree(l *sync.Mutex) {
	chainTwo(l)
}

func cyclePing(l *sync.Mutex) {
	cyclePong(l)
}

func cyclePong(l *sync.Mutex) {
	lockParam(l)
	cyclePing(l)
}

func relockGlobal() {
	lockGlobal()
}

func (c *Counter) inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Counter) incTwoDeep() {
	c.inc()
}

func (c *Counter) read() int {
	return c.n
}

func (c *Counter) relockMu() {
	c.mu.Lock()
	c.mu.Unlock()
}

func (c *Counter) grabOther(o *Counter) {
	withLock(&o.mu, c.relockMu)
}

func (n *Node) walk() {
	if n.next != nil {
		n.next.walk()
		lockParam(&n.next.mu)
	}
}

func (c *Counter) outerDeadlocks() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inc() // want `^call to inc locks c\.mu while it is already held`
}

func (c *Counter) outerTwoDeep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.incTwoDeep() // want `^call to incTwoDeep locks c\.mu while it is already held`
}

func (c *Counter) outerOtherMethod() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += c.read()
}

func directDepthZero() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	l.Lock() // want `^l is locked again while already held`
}

func directParenthesizedLiteral() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	(func() {
		l.Lock() // want `^l is locked again while already held`
	})()
}

func directOnGlobal() {
	global.Lock()
	defer global.Unlock()
	global.Lock() // want `^global is locked again while already held`
}

func directOnField() {
	var c Counter
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mu.Lock() // want `^c\.mu is locked again while already held`
}

func directThroughPointer() {
	var l sync.Mutex
	p := &l
	p.Lock()
	defer p.Unlock()
	p.Lock() // want `^p is locked again while already held`
}

func directNamedCallback() {
	var l sync.Mutex
	n := 0
	l.Lock()
	defer l.Unlock()
	l.Lock() // want `^l is locked again while already held`
	setOne(&n)
}

func directDeeplyNested(n int) {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	if n > 0 {
		for range 2 {
			l.Lock() // want `^l is locked again while already held`
		}
	}
}

func directThroughOtherLock() {
	var a, b sync.Mutex
	a.Lock()
	defer a.Unlock()
	b.Lock()
	defer b.Unlock()
	a.Lock() // want `^a is locked again while already held`
}

func directThreeLevels() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	l.Lock() // want `^l is locked again while already held`
	l.Lock() // want `^l is locked again while already held`
}

func depthOneGlobal() {
	global.Lock()
	defer global.Unlock()
	lockGlobal() // want `^call to lockGlobal locks global while it is already held`
}

func depthOneParameter() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	lockParam(&l) // want `^call to lockParam locks l while it is already held`
}

func depthOneMethodOnField() {
	var c Counter
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inc() // want `^call to inc locks c\.mu while it is already held`
}

func depthTwoParameter() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	lockParamTwoDeep(&l) // want `^call to lockParamTwoDeep locks l while it is already held`
}

func depthTwoGlobal() {
	global.Lock()
	defer global.Unlock()
	lockGlobalTwice() // want `^call to lockGlobalTwice locks global while it is already held`
}

func depthThreeParameter() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	chainThree(&l) // want `^call to chainThree locks l while it is already held`
}

func cyclePositive() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	cyclePing(&l) // want `^call to cyclePing locks l while it is already held`
}

func relock() {
	byName.Lock() // want `^byName is locked again while already held`
	byName.Unlock()
}

func namedCallbackPositive() {
	withLock(&byName, relock)
}

func (c *Counter) set() {
	c.mu.Lock() // want `^c\.mu is locked again while already held`
	c.mu.Unlock()
}

func methodValueCallbackPositive() {
	var c Counter
	withLock(&c.mu, c.set)
}

func directImmediateLiteral() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	func() {
		l.Lock() // want `^l is locked again while already held`
	}()
}

func directDeferredLiteral() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	defer func() {
		l.Lock() // want `^l is locked again while already held`
	}()
}

func goArgumentEvaluationPositive() {
	global.Lock()
	defer global.Unlock()
	go sink(lockAndReturn()) // want `^call to lockAndReturn locks global while it is already held`
}

func h() {
	withLock(&other, relockGlobal)
}

func outer() {
	global.Lock()
	defer global.Unlock()
	h() // want `^call to h locks global while it is already held`
}

func relockOuter() {
	withLock(&other, relockGlobal) // want `^call to withLock locks global while it is already held`
}

func outer2() {
	withLock(&global, relockOuter)
}

func methodCallbackTransitive() {
	var c, o Counter
	c.mu.Lock()
	defer c.mu.Unlock()
	c.grabOther(&o) // want `^call to grabOther locks c\.mu while it is already held`
}

func passDifferentReceiver() {
	var a, b sync.Mutex
	a.Lock()
	defer a.Unlock()
	b.Lock()
	b.Unlock()
}

func passDifferentGlobals() {
	global.Lock()
	defer global.Unlock()
	lockOther()
}

func passDifferentParameter() {
	var a, b sync.Mutex
	a.Lock()
	defer a.Unlock()
	lockParam(&b)
}

func passDifferentCounters() {
	var a, b Counter
	a.mu.Lock()
	defer a.mu.Unlock()
	b.inc()
}

func passDifferentField() {
	var w Wrapper
	w.mu.Lock()
	defer w.mu.Unlock()
	w.inner.inc()
}

func passSequential() {
	var l sync.Mutex
	l.Lock()
	l.Unlock()
	l.Lock()
	l.Unlock()
	lockParam(&l)
}

func passAfterLambda() {
	global.Lock()
	global.Unlock()
	lockGlobal()
}

func passShadowedReceiver() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	func() {
		var l sync.Mutex
		l.Lock()
		l.Unlock()
	}()
}

func passDeferredNestedLiteral() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	inner := func() {
		lockParam(&l)
	}
	_ = inner
}

func passUnmatchedReceiver() {
	var ls [2]sync.Mutex
	ls[0].Lock()
	defer ls[0].Unlock()
	ls[1].Lock()
	ls[1].Unlock()
}

func passGoSpawnedCall() {
	global.Lock()
	defer global.Unlock()
	go lockGlobal()
}

func passGoSpawnedLiteral() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	go func() {
		lockParam(&l)
	}()
}

func passTwoHopDifferent() {
	global.Lock()
	defer global.Unlock()
	lockOtherTwice()
}

func passDeferredDifferentLock() {
	var l sync.Mutex
	l.Lock()
	defer l.Unlock()
	defer lockOther()
}

func passRecursiveWalkTerminates() {
	n := &Node{next: &Node{}}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.walk()
}
