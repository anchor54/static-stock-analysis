package indicators

import (
	"math"
	"time"
)

// RelativeStrength returns the stock's return over `period` sessions divided by
// the index's return over the same sessions, minus one, for the latest common
// date (now) and for `back` common sessions earlier (ago). Only dates present
// in both series are used. Missing values are NaN.
func RelativeStrength(stock, index []Bar, period, back int) (now, ago float64) {
	idx := make(map[time.Time]float64, len(index))
	for _, b := range index {
		idx[b.Date] = b.Close
	}
	var s, i []float64
	for _, b := range stock {
		if c, ok := idx[b.Date]; ok {
			s = append(s, b.Close)
			i = append(i, c)
		}
	}
	at := func(t int) float64 {
		if t-period < 0 || t >= len(s) {
			return math.NaN()
		}
		if s[t-period] == 0 || i[t-period] == 0 || i[t] == 0 {
			return math.NaN()
		}
		return (s[t]/s[t-period])/(i[t]/i[t-period]) - 1
	}
	n := len(s)
	return at(n - 1), at(n - 1 - back)
}
