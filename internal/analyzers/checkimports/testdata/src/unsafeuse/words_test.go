package unsafeuse

import (
	"testing"
	"unsafe"
)

func TestWords(t *testing.T) {
	var x int
	if words(unsafe.Pointer(&x)) == 0 && wordSize > 1 {
		t.Log("pointer below one word")
	}
}
