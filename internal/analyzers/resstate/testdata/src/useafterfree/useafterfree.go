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
