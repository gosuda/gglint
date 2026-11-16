package handoffflow

import (
	"sync"
	"unsafe"
)

func withLock(mu *sync.Mutex, callback func()) {
	mu.Lock()
	defer mu.Unlock()
	callback()
}

func invoke(callback func()) { callback() }
func consume(int)            {}
func noop()                  {}

func lockAndValue(mu *sync.Mutex) int {
	mu.Lock()
	return 0
}

func deferredSpawn() {
	var mu sync.Mutex
	mu.Lock()
	defer func() { go mu.Unlock() }()
	mu.Lock() // want `mu is locked again while already held`
}

func deferredCallbackSpawn() {
	var mu sync.Mutex
	mu.Lock()
	defer invoke(func() { go mu.Unlock() })
	mu.Lock() // want `mu is locked again while already held`
}

func callbackDeferredSpawn() {
	var mu sync.Mutex
	withLock(&mu, func() {
		defer func() { go mu.Unlock() }()
		mu.Lock() // want `mu is locked again while already held`
	})
}

func invokeAfterDeferredSpawn(mu *sync.Mutex, callback func()) {
	mu.Lock()
	defer func() { go mu.Unlock() }()
	callback()
}

func callbackStillHeldAfterDefer() {
	var mu sync.Mutex
	invokeAfterDeferredSpawn(&mu, func() {
		mu.Lock() // want `mu is locked again while already held`
	})
}

func deferredBodyRunsAfterArguments() {
	var mu sync.Mutex
	mu.Lock()
	defer func(int) { go mu.Unlock() }(lockAndValue(&mu)) // want `call to lockAndValue locks mu while it is already held`
}

func ordinaryBodyRunsAfterArguments() {
	var mu sync.Mutex
	mu.Lock()
	func(int) { go mu.Unlock() }(lockAndValue(&mu)) // want `call to lockAndValue locks mu while it is already held`
}

func argumentReleasePrecedesBody() {
	var mu sync.Mutex
	mu.Lock()
	func(int) { mu.Lock() }(func() int {
		go mu.Unlock()
		return 0
	}())
}

func deferredArgumentSpawnsNow() {
	var mu sync.Mutex
	mu.Lock()
	defer consume(func() int {
		go mu.Unlock()
		return 0
	}())
	mu.Lock()
}

func deferredOperandSpawnsNow() {
	var mu sync.Mutex
	mu.Lock()
	defer func() func() {
		go mu.Unlock()
		return noop
	}()()
	mu.Lock()
}

func deferredOperandStillRunsBeforeLaterUnlock() {
	var mu sync.Mutex
	mu.Lock()
	defer func() func() {
		mu.Lock() // want `mu is locked again while already held`
		return noop
	}()()
	defer mu.Unlock()
}

func deferredArgumentStillRunsBeforeLaterUnlock() {
	var mu sync.Mutex
	mu.Lock()
	defer consume(lockAndValue(&mu)) // want `call to lockAndValue locks mu while it is already held`
	defer mu.Unlock()
}

func deferredLockBeforeUnlock() {
	var mu sync.Mutex
	mu.Lock()
	defer func() { mu.Lock() }()
	defer mu.Unlock()
}

func deferredLockAfterUnlock() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	defer func() { mu.Lock() }() // want `mu is locked again while already held`
}

func ifElse(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		go mu.Unlock()
	} else {
		mu.Lock() // want `mu is locked again while already held`
	}
}

func ifThen(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		mu.Lock() // want `mu is locked again while already held`
	} else {
		go mu.Unlock()
	}
}

func elseIf(cond, other bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		go mu.Unlock()
	} else if other {
		mu.Lock() // want `mu is locked again while already held`
	}
}

func callbackIfElse(cond bool) {
	var mu sync.Mutex
	withLock(&mu, func() {
		if cond {
			go mu.Unlock()
		} else {
			mu.Lock() // want `mu is locked again while already held`
		}
	})
}

func invokeInHeldElse(mu *sync.Mutex, cond bool, callback func()) {
	mu.Lock()
	if cond {
		go mu.Unlock()
	} else {
		callback()
	}
}

func callbackStillHeldInElse(cond bool) {
	var mu sync.Mutex
	invokeInHeldElse(&mu, cond, func() {
		mu.Lock() // want `mu is locked again while already held`
	})
}

func conditionalReleaseMayLeaveHeld(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func bothArmsRelease(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		go mu.Unlock()
	} else {
		go mu.Unlock()
	}
	mu.Lock()
}

func conditionReleasesBeforeEitherArm(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if func() bool {
		go mu.Unlock()
		return cond
	}() {
		mu.Lock()
	} else {
		mu.Lock()
	}
}

func switchArms(n int) {
	var mu sync.Mutex
	mu.Lock()
	switch n {
	case 0:
		go mu.Unlock()
	default:
		mu.Lock() // want `mu is locked again while already held`
	}
}

func typeSwitchArms(value any) {
	var mu sync.Mutex
	mu.Lock()
	switch value.(type) {
	case int:
		go mu.Unlock()
	default:
		mu.Lock() // want `mu is locked again while already held`
	}
}

func selectArms(ready <-chan struct{}) {
	var mu sync.Mutex
	mu.Lock()
	select {
	case <-ready:
		go mu.Unlock()
	default:
		mu.Lock() // want `mu is locked again while already held`
	}
}

func selectOperandsRunBeforeChoice(ready chan<- int) {
	var mu sync.Mutex
	mu.Lock()
	select {
	case ready <- func() int {
		go mu.Unlock()
		return 0
	}():
		mu.Lock()
	default:
		mu.Lock()
	}
}

func unconditionalReleaseAfterJoin(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		noop()
	} else {
		noop()
	}
	go mu.Unlock()
	mu.Lock()
}

func constantTrueArm() {
	var mu sync.Mutex
	mu.Lock()
	if true {
		go mu.Unlock()
	} else {
		mu.Lock()
	}
	mu.Lock()
}

func constantFalseArm() {
	var mu sync.Mutex
	mu.Lock()
	if false {
		go mu.Unlock()
	} else {
		mu.Lock() // want `mu is locked again while already held`
	}
}

func returningElseDoesNotReachJoin(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		go mu.Unlock()
	} else {
		return
	}
	mu.Lock()
}

func returningThenDoesNotReachJoin(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		return
	} else {
		go mu.Unlock()
	}
	mu.Lock()
}

func bothArmsReturn(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond {
		return
	} else {
		return
	}
	mu.Lock()
}

func literalReturnsPreserveCallerHolding(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	func() {
		if cond {
			return
		} else {
			return
		}
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func conditionalOperandMayNotRelease(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	if cond && func() bool {
		go mu.Unlock()
		return true
	}() {
		noop()
	} else {
		mu.Lock() // want `mu is locked again while already held`
	}
}

func unconditionalAndOperandReleases() {
	var mu sync.Mutex
	mu.Lock()
	if true && func() bool {
		go mu.Unlock()
		return true
	}() {
		mu.Lock()
	} else {
		mu.Lock()
	}
}

func unconditionalOrOperandReleases() {
	var mu sync.Mutex
	mu.Lock()
	if false || func() bool {
		go mu.Unlock()
		return true
	}() {
		mu.Lock()
	} else {
		mu.Lock()
	}
}

func conditionalLoopMayNotRun(n int) {
	var mu sync.Mutex
	mu.Lock()
	for i := 0; i < n; i++ {
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func countedLoopRuns() {
	var mu sync.Mutex
	mu.Lock()
	for i := 0; i < 1; i++ {
		go mu.Unlock()
	}
	mu.Lock()
}

func constantFalseLoop() {
	var mu sync.Mutex
	mu.Lock()
	for false {
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func rangeMayNotRun(items []int) {
	var mu sync.Mutex
	mu.Lock()
	for range items {
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func selectLaterOperandRunsBeforeEarlierBody(ready <-chan int, send chan<- int) {
	var mu sync.Mutex
	mu.Lock()
	select {
	case <-ready:
		mu.Lock()
	case send <- func() int {
		go mu.Unlock()
		return 0
	}():
		noop()
	}
}

func selectAssignmentOnlyRunsInItsArm(ready <-chan int) {
	var mu sync.Mutex
	var value int
	mu.Lock()
	select {
	case *func() *int {
		go mu.Unlock()
		return &value
	}() = <-ready:
		noop()
	default:
		mu.Lock() // want `mu is locked again while already held`
	}
}

func switchCaseExpressionIsConditional(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	switch {
	case cond:
		go mu.Unlock()
	case func() bool {
		mu.Lock() // want `mu is locked again while already held`
		return true
	}():
		noop()
	}
}

func literalReturnBeforeEmptySelect(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	func() {
		if cond {
			return
		}
		select {}
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func constantSwitchFallthrough() {
	var mu sync.Mutex
	mu.Lock()
	switch 0 {
	case 0:
		fallthrough
	case 1:
		mu.Lock() // want `mu is locked again while already held`
	}
}

func constantSwitchKnownArm() {
	var mu sync.Mutex
	mu.Lock()
	switch 0 {
	case 0:
		go mu.Unlock()
	case 1:
		mu.Lock()
	}
	mu.Lock()
}

func constantSwitchNoMatch() {
	var mu sync.Mutex
	mu.Lock()
	switch 0 {
	case 1:
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func countedLoopMayBreak(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	for i := 0; i < 1; i++ {
		if cond {
			break
		}
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func counterConditionUsesAnotherVariable(n int) {
	var mu sync.Mutex
	mu.Lock()
	for i := 0; n < 1; i++ {
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func constantNameIsShadowed(true bool) {
	var mu sync.Mutex
	mu.Lock()
	if true {
		go mu.Unlock()
	} else {
		mu.Lock() // want `mu is locked again while already held`
	}
}

func handoffRemainsLocalToDeferredBody() {
	var mu sync.Mutex
	mu.Lock()
	defer func() {
		go mu.Unlock()
		mu.Lock()
	}()
	mu.Lock() // want `mu is locked again while already held`
}

func maybeInvoke(cond bool, callback func()) {
	if cond {
		callback()
	}
}

func callbackMayNotRun(cond bool) {
	var mu sync.Mutex
	mu.Lock()
	maybeInvoke(cond, func() { go mu.Unlock() })
	mu.Lock() // want `mu is locked again while already held`
}

func secondBeforeFirst(first, second func()) {
	second()
	first()
}

func callbackOrderIsNotArgumentOrder() {
	var mu sync.Mutex
	mu.Lock()
	secondBeforeFirst(func() { go mu.Unlock() }, func() {
		mu.Lock() // want `mu is locked again while already held`
	})
}

func constantSizeofDoesNotSpawn() {
	var mu sync.Mutex
	mu.Lock()
	_ = unsafe.Sizeof(func() int {
		go mu.Unlock()
		return 0
	}())
	mu.Lock() // want `mu is locked again while already held`
}

func constantAlignofDoesNotSpawn() {
	var mu sync.Mutex
	mu.Lock()
	_ = unsafe.Alignof(func() int {
		go mu.Unlock()
		return 0
	}())
	mu.Lock() // want `mu is locked again while already held`
}

func constantSizeofDoesNotLock() {
	var mu sync.Mutex
	mu.Lock()
	_ = unsafe.Sizeof(lockAndValue(&mu))
	mu.Unlock()
}

func Sizeof(int) uintptr { return 0 }

func shadowedSizeofEvaluatesArguments() {
	unsafe := struct{ Sizeof func(int) uintptr }{Sizeof}
	var mu sync.Mutex
	mu.Lock()
	_ = unsafe.Sizeof(func() int {
		go mu.Unlock()
		return 0
	}())
	mu.Lock()
}

func genericSizeofEvaluatesArgument[T any](value T) {
	var mu sync.Mutex
	mu.Lock()
	_ = unsafe.Sizeof(func() T {
		go mu.Unlock()
		return value
	}())
	mu.Lock()
}

func compoundLoopInitMayNotEnter() {
	var mu sync.Mutex
	mu.Lock()
	i := 2
	for i += 0; i < 1; i++ {
		go mu.Unlock()
	}
	mu.Lock() // want `mu is locked again while already held`
}

func assignmentLoopInitStillEnters() {
	var mu sync.Mutex
	mu.Lock()
	i := 2
	for i = 0; i < 1; i++ {
		go mu.Unlock()
	}
	mu.Lock()
}
