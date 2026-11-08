package dupes

func scoreOrders(values, weights []int, limit int) (int, int) { // want `duplicate of dupes.go:58-111`
	total, count := 0, 0
	for i, v := range values {
		if v > limit {
			total += v * i
			count++
		} else if v < -limit {
			total -= v * i
			count--
		} else {
			total += v
		}
	}
	for i := range values {
		switch values[i] % 3 {
		case 0:
			total += values[i] / 3
		case 1:
			total -= values[i] * 2
		default:
			count += values[i] % 7
		}
	}
	for j, w := range weights {
		if w == 0 {
			continue
		}
		if j%2 == 0 {
			total += w * limit
		} else {
			total -= w + limit
		}
		for k := range w {
			if k > count {
				break
			}
			count += k * j
		}
	}
	for _, w := range weights {
		for _, v := range values {
			if v+w > limit {
				total += (v + w) / 2
			}
		}
	}
	if total > limit*count {
		total = limit * count
	}
	if count < 0 {
		count = -count
	}
	return total, count
}

func scoreInvoices(values, weights []int, limit int) (int, int) { // want `duplicate of dupes.go:3-56`
	total, count := 0, 0
	for i, v := range values {
		if v > limit {
			total += v * i
			count++
		} else if v < -limit {
			total -= v * i
			count--
		} else {
			total += v
		}
	}
	for i := range values {
		switch values[i] % 3 {
		case 0:
			total += values[i] / 3
		case 1:
			total -= values[i] * 2
		default:
			count += values[i] % 7
		}
	}
	for j, w := range weights {
		if w == 0 {
			continue
		}
		if j%2 == 0 {
			total += w * limit
		} else {
			total -= w + limit
		}
		for k := range w {
			if k > count {
				break
			}
			count += k * j
		}
	}
	for _, w := range weights {
		for _, v := range values {
			if v+w > limit {
				total += (v + w) / 2
			}
		}
	}
	if total > limit*count {
		total = limit * count
	}
	if count < 0 {
		count = -count
	}
	return total, count
}
