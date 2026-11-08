package unique

func clampLow(v, limit int) int {
	if v < limit {
		return limit
	}
	return v
}

func clampHigh(v, limit int) int {
	if v < limit {
		return limit
	}
	return v
}

func histogram(values []int, buckets int) []int {
	counts := make([]int, buckets)
	for _, v := range values {
		switch {
		case v < 0:
			counts[0]++
		case v >= buckets:
			counts[buckets-1]++
		default:
			counts[v]++
		}
	}
	return counts
}
