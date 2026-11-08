package a

import "fmt"

func logf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func Bad() {
	logf("%d\n", "x")
}
