package conv

type celsius float64

func same(x int) int {
	return int(x) // want `unnecessary conversion`
}

func narrowed(x int64) int {
	return int(x)
}

func named(x float64) celsius {
	return celsius(x)
}

func unnamed(c celsius) float64 {
	return float64(c)
}
