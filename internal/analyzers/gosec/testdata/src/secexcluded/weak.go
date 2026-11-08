package secexcluded

import "crypto/md5"

func Weak(b []byte) [16]byte {
	return md5.Sum(b)
}
