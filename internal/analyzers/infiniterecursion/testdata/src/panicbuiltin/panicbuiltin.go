package panicbuiltin

import (
	"log"
	"os"
	"runtime"
)

func builtin(n int) int {
	panic("stop")
	return builtin(n)
}

func parenthesized(n int) int {
	(panic)("stop")
	return parenthesized(n)
}

func branch(n int) int {
	if n > 0 {
		(panic)("stop")
	}
	return branch(n)
}

func goexit(n int) int {
	(runtime.Goexit)()
	return goexit(n)
}

func exit(n int) int {
	(os.Exit)(1)
	return exit(n)
}

func fatal(n int) int {
	(log.Fatal)("stop")
	return fatal(n)
}
