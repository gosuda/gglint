package c

import "fmt"

func logf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func AlsoBad() {
	logf("%d\n", "y")
}
