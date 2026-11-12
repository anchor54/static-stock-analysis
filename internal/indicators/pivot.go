package indicators

import "time"

// Pivot holds classic floor-trader pivot levels and the bar they came from.
type Pivot struct {
	P, R1, R2, R3, S1, S2, S3 float64
	From                      time.Time
}

// ClassicPivot computes classic pivot levels from one bar's high, low and close.
func ClassicPivot(h, l, c float64) Pivot {
	p := (h + l + c) / 3
	return Pivot{
		P:  p,
		R1: 2*p - l,
		S1: 2*p - h,
		R2: p + (h - l),
		S2: p - (h - l),
		R3: h + 2*(p-l),
		S3: l - 2*(h-p),
	}
}

// PivotInForce returns the pivot in force for the current (last, possibly
// incomplete) bar, computed from the previous completed bar. ok is false when
// there is no previous bar.
func PivotInForce(bars []Bar) (pv Pivot, ok bool) {
	if len(bars) < 2 {
		return Pivot{}, false
	}
	src := bars[len(bars)-2]
	pv = ClassicPivot(src.High, src.Low, src.Close)
	pv.From = src.Date
	return pv, true
}
