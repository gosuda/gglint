package sec

import "crypto/sha256"

func Strong(b []byte) [32]byte {
	return sha256.Sum256(b)
}
