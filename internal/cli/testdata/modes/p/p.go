package p

import "fmt"

func logf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func NoReason() {
	logf("%d\n", "noreason") //nolint:printf
}

func Bare() {
	fmt.Println("bare") //nolint
}

func Spaced() {
	logf("%d\n", "spaced") // nolint:printf
}

func Unknown() {
	logf("%d\n", "unknown") //nolint:nope // reason
}

func Unused() {
	fmt.Println("unused") //nolint:printf // reason
}

func Unselected() {
	fmt.Println("unselected") //nolint:chanblock // reason
}

func Typecheck() {
	fmt.Println("typecheck") //nolint:typecheck // reason
}

func SelfCompare(n int) bool {
	return n == n //nolint:staticcheck:SA4000 // reason
}

func OtherCheck(n int) bool {
	return n == n //nolint:staticcheck:SA4001 // reason
}

func WholeRule(n int) bool {
	return n == n //nolint:staticcheck // reason
}
