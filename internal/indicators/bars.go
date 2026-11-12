// Package indicators holds the pure indicator engine. It never touches the
// network, so every formula can be unit-tested on fixed data.
package indicators

import (
	"math"
	"time"
)

// Bar is one OHLCV bar. Date is the calendar date (exchange-local) of the
// bar's last trading session, stored at midnight UTC.
type Bar struct {
	Date   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Closes returns the close of every bar.
func Closes(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

// Weekly aggregates daily bars into weeks ending on Friday. The current week
// is kept as a live, incomplete bar.
func Weekly(daily []Bar) []Bar { return resample(daily, weekEnd) }

// Monthly aggregates daily bars into calendar months. The current month is
// kept as a live, incomplete bar.
func Monthly(daily []Bar) []Bar { return resample(daily, monthStart) }

func weekEnd(d time.Time) time.Time {
	offset := (int(time.Friday) - int(d.Weekday()) + 7) % 7
	y, m, day := d.AddDate(0, 0, offset).Date()
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

func monthStart(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// resample groups consecutive bars by period key: open is the first open, high
// the max, low the min, close the last close, volume the sum. The result's
// Date is the last trading date inside the period.
func resample(daily []Bar, key func(time.Time) time.Time) []Bar {
	var out []Bar
	var cur time.Time
	for _, b := range daily {
		k := key(b.Date)
		if len(out) == 0 || !k.Equal(cur) {
			out = append(out, b)
			cur = k
			continue
		}
		last := &out[len(out)-1]
		last.High = math.Max(last.High, b.High)
		last.Low = math.Min(last.Low, b.Low)
		last.Close = b.Close
		last.Volume += b.Volume
		last.Date = b.Date
	}
	return out
}

// PctDist returns (price ÷ level − 1) × 100, or NaN when level is unusable.
func PctDist(price, level float64) float64 {
	if level == 0 || math.IsNaN(level) || math.IsNaN(price) {
		return math.NaN()
	}
	return (price/level - 1) * 100
}

// Last returns the final element of xs, or NaN when xs is empty.
func Last(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	return xs[len(xs)-1]
}

func nanSlice(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	return out
}
