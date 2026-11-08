package test1

// +checkescape:heap
//
//go:noinline
func reportedAllocation() *int {
	return new(int) // want "heap: explicit allocation"
}
