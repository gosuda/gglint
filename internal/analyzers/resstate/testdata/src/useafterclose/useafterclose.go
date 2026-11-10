package useafterclose

import (
	"os"
)

func useaftercloseReported(p string, b []byte) {
	f, _ := os.Open(p)
	f.Close()
	f.Read(b) // want "is closed on every path to this call"
}

func useaftercloseSilent(p string, b []byte) {
	f, _ := os.Open(p)
	defer f.Close()
	f.Read(b)
}

func useaftercloseSilentEscape(p string, ch chan *os.File) *os.File {
	f, _ := os.Open(p)
	f.Close()
	_ = &f
	ch <- f
	g := f
	_ = g
	return f
}
