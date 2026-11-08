//go:build unix

package config

import (
	"os"
	"syscall"
)

func ownedByOther(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) != int64(os.Getuid())
}
