package indicators

import "math"

// VWAP returns the volume-weighted average of each bar's typical price
// (H+L+C)/3 over bars, normally one session's intraday bars. Bars without
// volume are ignored; it is NaN when there is no volume at all.
func VWAP(bars []Bar) float64 {
	var pv, vol float64
	for _, b := range bars {
		if b.Volume <= 0 {
			continue
		}
		pv += (b.High + b.Low + b.Close) / 3 * b.Volume
		vol += b.Volume
	}
	if vol == 0 {
		return math.NaN()
	}
	return pv / vol
}
