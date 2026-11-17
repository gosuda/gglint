//go:build !windows

package errdrop

import "syscall"

func verdictReportedSyscallClose(fd int) error {
	syscall.Close(fd) // want "the error of syscall.Close is dropped and the next statement returns nil"
	return nil
}
