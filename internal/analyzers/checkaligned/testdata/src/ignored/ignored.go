// Package ignored opts out of checkaligned.
//
// +checkalignedignore
package ignored

import "sync/atomic"

func increment(n *int64) int64 {
	return atomic.AddInt64(n, 1)
}
