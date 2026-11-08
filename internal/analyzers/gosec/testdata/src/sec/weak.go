package sec

import "crypto/md5" // want "G501: Blocklisted import crypto/md5: weak cryptographic primitive"

func Weak(b []byte) [16]byte {
	return md5.Sum(b) // want "G401: Use of weak cryptographic primitive"
}
