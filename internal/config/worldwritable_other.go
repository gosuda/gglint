//go:build !windows

package config

import (
	"errors"
	"os"
)

func worldWritable(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0o002 != 0, nil
}
