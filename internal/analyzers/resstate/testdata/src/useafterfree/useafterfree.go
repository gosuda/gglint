package useafterfree

import (
	"syscall"
)

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

func useafterfreeSilentSliceArg() int {
	b := make([]byte, 4096)
	n := 4
	syscall.Munmap(b[:n])
	return n
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
		_ = b[0] // want "is used after it was freed"
	}
}

func useafterfreeReportedReassign() {
	b := make([]byte, 4096)
	syscall.Munmap(b)
	b = b[:0] // want "is used after it was freed"
	_ = b
}
