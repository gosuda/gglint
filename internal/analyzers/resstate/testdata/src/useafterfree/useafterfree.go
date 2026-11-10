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
