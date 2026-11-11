package useafterfree

import (
	"syscall"
)

var sink byte
func useafterfreeReported() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b[0] = 1 // want "is used after it was freed"
}

func useafterfreeSilent() {
	b := make([]byte, 4096)
	b[0] = 1
	syscall.Munmap(b)
}


func useafterfreeSilentRangeAssign(chs chan []byte) int {
	b := make([]byte, 4)
	syscall.Munmap(b)
	n := 0
	for b = range chs {
		n += len(b)
	}
	return n
}

func useafterfreeSilentFieldObj() int {
	type holder struct {
		b []byte
	}
	x := holder{b: make([]byte, 4)}
	y := holder{b: make([]byte, 4)}
	syscall.Munmap(x.b)
	return len(y.b)
}

func useafterfreeReportedLoop() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	for range 2 {
		sink = b[0] // want "is used after it was freed"
	}
	_ = sink
}

func useafterfreeReportedReassign() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b = append(b, 0) // want "is used after it was freed"
	_ = b
}

func useafterfreeSilentReassignNew() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b = make([]byte, 4096)
	_ = b
}

func useFreeBytes(p []byte) byte {
	return p[0]
}

func useafterfreeReportedCallArg() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	useFreeBytes(b) // want "is used after it was freed"
}

func useafterfreeSilentElementArg() {
	bufs := [][]byte{make([]byte, 4096), make([]byte, 4096)}
	syscall.Munmap(bufs[0])
	syscall.Munmap(bufs[1])
}

func freeOther() ([]byte, error) { return nil, nil }

func useafterfreeSilentTempReassign() byte {
	b := make([]byte, 4096)
	p := b
	p, _ = freeOther()
	syscall.Munmap(p)
	return b[0]
}

func useafterfreeSilentPartialSlice() int {
	b, err := syscall.Mmap(-1, 0, 8192, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0
	}
	syscall.Munmap(b[:2048])
	return int(b[4096])
}

func useafterfreeSilentLenAfterFree() int {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	return len(b)
}

func useafterfreeSilentNilCompareAfterFree() bool {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	if b == nil {
		return true
	}
	return false
}
func useafterfreeSilentSliceHeader() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	_ = b[:0]
}

func useafterfreeReportedSliceCall() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	useFreeBytes(b[:]) // want "is used after it was freed"
}

func useafterfreeReportedSliceStore() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	x := b[:] // want "is used after it was freed"
	_ = x
}

func useafterfreeSilentSelfSlice() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b = b[:0]
	_ = b
}
func useafterfreeReportedSelfSliceAppend() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b = b[:0]
	b = append(b, 0) // want "is used after it was freed"
	_ = b
}

func useafterfreeSilentIntBound() {
	b := make([]byte, 4096)
	n := 2048
	syscall.Munmap(b[:n])
	n++
	_ = n
	_ = b
}

func useafterfreeSilentIntRead() int {
	b := make([]byte, 4096)
	n := 2048
	syscall.Munmap(b[:n])
	_ = b
	return n
}

func useafterfreeReportedAppendFresh() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b = append(b[:0:0], 1) // want "is used after it was freed"
	b[0] = 1
	_ = b
}

func useafterfreeSilentAddrDrop() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	p := &b
	_ = p
}

func useafterfreeReportedWholeSlice() {
	b := make([]byte, 4096)
	syscall.Munmap(b[:])
	_ = b[0] // want "is used after it was freed"
}

func useafterfreeSilentNestedReassign() int {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	func() {
		n := func() int { b = make([]byte, 4096); return len(b) }()
		_ = n
	}()
	v := int(b[0])
	syscall.Munmap(b)
	return v
}

func useafterfreeSilentTopReassign() int {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	ok := func() bool { b = make([]byte, 4096); return b != nil }()
	_ = ok
	v := int(b[0])
	syscall.Munmap(b)
	return v
}

func useafterfreeSilentRemappedAlias() (byte, error) {
	a, err := syscall.Mmap(-1, 0, 4096, syscall.PROT_READ, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0, err
	}
	old := a
	a, err = syscall.Mmap(-1, 0, 4096, syscall.PROT_READ, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0, err
	}
	syscall.Munmap(old)
	return a[0], nil
}

func useafterfreeSilentRangeTempOverwrite() byte {
	a := make([]byte, 4096)
	old := a
	chunks := [][]byte{make([]byte, 4096)}
	for _, old = range chunks {
	}
	syscall.Munmap(old)
	return a[0]
}

func useafterfreeReportedInPlaceUse() (byte, error) {
	b, err := syscall.Mmap(-1, 0, 4096, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0, err
	}
	syscall.Munmap(b)
	func() { b[0] = 1 }() // want "is used after it was freed"
	return 0, nil
}

func useafterfreeReportedAliasUseBeforeRebind() (byte, error) {
	a, err := syscall.Mmap(-1, 0, 4096, syscall.PROT_READ, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0, err
	}
	old := a
	syscall.Munmap(old)
	v := a[0] // want "is used after it was freed"
	a, err = syscall.Mmap(-1, 0, 4096, syscall.PROT_READ, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0, err
	}
	return v + a[0], nil
}

func useafterfreeReportedAliasUseBeforeSlice() byte {
	a := make([]byte, 4096)
	old := a
	syscall.Munmap(old)
	b := a[0] // want "is used after it was freed"
	a = a[1:]
	_ = a
	return b
}

func useafterfreeSilentLoopRebindAfterRelease() byte {
	a := make([]byte, 4096)
	old := a
	for range 1 {
		syscall.Munmap(old)
		a = make([]byte, 4096)
		_ = a[0]
	}
	return a[0]
}

func useafterfreeSilentLitRemapAlias() (byte, error) {
	a, err := syscall.Mmap(-1, 0, 4096, syscall.PROT_READ, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return 0, err
	}
	old := a
	func() { a, err = syscall.Mmap(-1, 0, 4096, syscall.PROT_READ, syscall.MAP_ANON|syscall.MAP_PRIVATE) }()
	if err != nil {
		return 0, err
	}
	syscall.Munmap(old)
	return a[0], nil
}
