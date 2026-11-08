//go:build !unix

package config

import "os"

func ownedByOther(os.FileInfo) bool {
	return false
}
