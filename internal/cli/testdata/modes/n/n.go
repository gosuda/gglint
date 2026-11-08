package n

import "fmt"

func logf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func Trailing() {
	logf("%d\n", "trailing") //nolint:printf // reason
	logf("%d\n", "below trailing")
}

func Alone() {
	//nolint:printf // reason
	logf("%d %s\n", "first line",
		fmt.Sprintf("%d", "last line"))
	logf("%d\n", "below statement")
}
