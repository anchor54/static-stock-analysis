package indicators

import "math"

// SMA returns the plain mean of the last n values, or NaN with fewer than n.
func SMA(values []float64, n int) float64 {
	if n <= 0 || len(values) < n {
		return math.NaN()
	}
	var sum float64
	for _, v := range values[len(values)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// SMASeries returns the rolling n-period mean for every value; the first n−1
// entries are NaN.
func SMASeries(values []float64, n int) []float64 {
	out := nanSlice(len(values))
	if n <= 0 || len(values) < n {
		return out
	}
	var sum float64
	for i, v := range values {
		sum += v
		if i >= n {
			sum -= values[i-n]
		}
		if i >= n-1 {
			out[i] = sum / float64(n)
		}
	}
	return out
}
