package indicators

import (
	"math"
	"testing"
	"time"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (±%v)", name, got, want, tol)
	}
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Wilder RSI reference series (StockCharts "RSI" ChartSchool worksheet).
func TestRSIWilderReference(t *testing.T) {
	closes := []float64{44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08,
		45.89, 46.03, 45.61, 46.28, 46.28, 46.00, 46.03, 46.41, 46.22, 45.64,
		46.21, 46.25, 45.71, 46.45, 45.78, 45.35, 44.03, 44.18, 44.22, 44.57,
		43.42, 42.66, 43.13}
	want := []float64{70.53, 66.32, 66.55, 69.41, 66.36, 57.97, 62.93, 63.26, 56.06, 62.38,
		54.71, 50.42, 39.99, 41.46, 41.87, 45.46, 37.30, 33.08, 37.77}
	rsi := RSI(closes, 14)
	for i := 0; i < 14; i++ {
		if !math.IsNaN(rsi[i]) {
			t.Fatalf("rsi[%d] = %v, want NaN before the first full period", i, rsi[i])
		}
	}
	// Exact (unrounded) first value.
	near(t, "first rsi", rsi[14], 70.4641, 1e-3)
	// The worksheet rounds the intermediate averages to 2 decimals, so its
	// published values drift by up to ~0.07 from exact Wilder smoothing.
	for i, w := range want {
		near(t, "rsi", rsi[14+i], w, 0.1)
	}
}

func TestRSIEdgeCases(t *testing.T) {
	up := make([]float64, 20)
	flat := make([]float64, 20)
	for i := range up {
		up[i] = float64(i + 1)
		flat[i] = 10
	}
	near(t, "all gains", Last(RSI(up, 14)), 100, 0)
	near(t, "no change", Last(RSI(flat, 14)), 50, 0)
	if !math.IsNaN(Last(RSI(up[:14], 14))) {
		t.Error("RSI with only 14 closes should be NaN")
	}
}

func TestSMA(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5}
	near(t, "SMA3", SMA(v, 3), 4, 1e-12)
	if !math.IsNaN(SMA(v, 6)) {
		t.Error("SMA with too few values should be NaN")
	}
	s := SMASeries(v, 2)
	if !math.IsNaN(s[0]) {
		t.Error("first SMASeries value should be NaN")
	}
	near(t, "SMASeries[4]", s[4], 4.5, 1e-12)
	near(t, "PctDist", PctDist(110, 100), 10, 1e-9)
}

func TestResample(t *testing.T) {
	// Thu 2 Oct 2025 … Tue 4 Nov 2025 trading days (no weekends).
	var daily []Bar
	for d := day(2025, 10, 2); !d.After(day(2025, 11, 4)); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		v := float64(d.Day())
		daily = append(daily, Bar{Date: d, Open: v, High: v + 1, Low: v - 1, Close: v + 0.5, Volume: 1})
	}

	w := Weekly(daily)
	// Weeks: ends 3 Oct (2 days), 10, 17, 24, 31 Oct, then live week with 3, 4 Nov.
	if len(w) != 6 {
		t.Fatalf("weekly bars = %d, want 6", len(w))
	}
	first := w[0]
	if !first.Date.Equal(day(2025, 10, 3)) || first.Open != 2 || first.High != 4 || first.Low != 1 || first.Close != 3.5 || first.Volume != 2 {
		t.Errorf("first week = %+v", first)
	}
	if last := w[5]; !last.Date.Equal(day(2025, 11, 4)) || last.Open != 3 || last.Close != 4.5 || last.Volume != 2 {
		t.Errorf("live week = %+v", last)
	}

	m := Monthly(daily)
	if len(m) != 2 {
		t.Fatalf("monthly bars = %d, want 2", len(m))
	}
	if oct := m[0]; !oct.Date.Equal(day(2025, 10, 31)) || oct.Open != 2 || oct.High != 32 || oct.Low != 1 || oct.Close != 31.5 || oct.Volume != 22 {
		t.Errorf("October = %+v", oct)
	}
}

func TestClassicPivot(t *testing.T) {
	p := ClassicPivot(110, 90, 100)
	for name, got := range map[string][2]float64{
		"P": {p.P, 100}, "R1": {p.R1, 110}, "S1": {p.S1, 90}, "R2": {p.R2, 120},
		"S2": {p.S2, 80}, "R3": {p.R3, 130}, "S3": {p.S3, 70},
	} {
		near(t, name, got[0], got[1], 1e-9)
	}

	bars := []Bar{
		{Date: day(2025, 1, 1), High: 110, Low: 90, Close: 100},
		{Date: day(2025, 1, 2), High: 200, Low: 150, Close: 180},
	}
	pv, ok := PivotInForce(bars)
	if !ok || pv.P != 100 || !pv.From.Equal(day(2025, 1, 1)) {
		t.Errorf("PivotInForce = %+v, %v; want the previous bar's pivot", pv, ok)
	}
	if _, ok := PivotInForce(bars[:1]); ok {
		t.Error("PivotInForce with one bar should not be ok")
	}
}

func TestRelativeStrength(t *testing.T) {
	var stock, index []Bar
	for i := 0; i < 70; i++ {
		d := day(2025, 1, 1).AddDate(0, 0, i)
		stock = append(stock, Bar{Date: d, Close: 100 * math.Pow(1.01, float64(i))})
		if i == 30 {
			continue // a date missing from the index must be skipped for both
		}
		index = append(index, Bar{Date: d, Close: 1000 * math.Pow(1.005, float64(i))})
	}
	now, ago := RelativeStrength(stock, index, 55, 5)
	// 69 common dates. Latest uses i=69 vs the common date 55 steps earlier (i=13).
	near(t, "RS now", now, math.Pow(1.01, 56)/math.Pow(1.005, 56)-1, 1e-9)
	// 5 sessions earlier: i=64 vs i=8.
	near(t, "RS ago", ago, math.Pow(1.01, 56)/math.Pow(1.005, 56)-1, 1e-9)

	now, _ = RelativeStrength(stock[:50], index, 55, 5)
	if !math.IsNaN(now) {
		t.Error("RS with too little history should be NaN")
	}
}

func TestVWAP(t *testing.T) {
	bars := []Bar{
		{High: 102, Low: 98, Close: 100, Volume: 100},  // typical 100
		{High: 106, Low: 100, Close: 106, Volume: 300}, // typical 104
		{High: 500, Low: 400, Close: 450, Volume: 0},   // no volume: ignored
		{High: 110, Low: 104, Close: 107, Volume: 100}, // typical 107
	}
	// (100×100 + 104×300 + 107×100) ÷ 500 = 103.8
	near(t, "VWAP", VWAP(bars), 103.8, 1e-9)
	if !math.IsNaN(VWAP(bars[2:3])) || !math.IsNaN(VWAP(nil)) {
		t.Error("VWAP without volume should be NaN")
	}
}

func series(n int, f func(i int) float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

func TestWeinsteinStage(t *testing.T) {
	cfg := DefaultStageConfig()
	cases := []struct {
		name   string
		closes []float64
		rs     float64
		num    int
		label  string
	}{
		{"steady advance", series(60, func(i int) float64 { return 100 * math.Pow(1.02, float64(i)) }), 0.1, 2, "Advancing"},
		{"pullback", append(series(59, func(i int) float64 { return 100 * math.Pow(1.02, float64(i)) }), 236), 0.1, 2, "Advancing (pullback)"},
		{"topping", append(series(59, func(i int) float64 { return 100 * math.Pow(1.02, float64(i)) }), 180), 0.1, 3, "Topping"},
		{"decline", series(60, func(i int) float64 { return 100 * math.Pow(0.98, float64(i)) }), -0.1, 4, "Declining"},
		{"bounce", append(series(59, func(i int) float64 { return 100 * math.Pow(0.98, float64(i)) }), 43), -0.1, 4, "Declining (bounce)"},
		{"basing", append(series(59, func(i int) float64 { return 100 * math.Pow(0.98, float64(i)) }), 60), -0.1, 1, "Basing"},
		{"flat after rise", append(series(30, func(i int) float64 { return 50 + float64(i)*2 }), series(30, func(int) float64 { return 110 })...), 0, 3, "Topping"},
		{"flat for long", series(60, func(i int) float64 { return 100 + float64(i%2) }), 0, 1, "Basing"},
		{"early advance", append(series(50, func(int) float64 { return 100 }), series(10, func(i int) float64 { return 100 * math.Pow(1.08, float64(i+1)) })...), 0.2, 2, "Advancing (early)"},
	}
	for _, c := range cases {
		st, ok := WeinsteinStage(c.closes, c.rs, cfg)
		if !ok {
			t.Errorf("%s: not ok", c.name)
			continue
		}
		if st.Number != c.num || st.Label != c.label {
			t.Errorf("%s: got %d %q, want %d %q (%s)", c.name, st.Number, st.Label, c.num, c.label, st.Reason)
		}
	}

	st, _ := WeinsteinStage(cases[0].closes, -0.05, cfg)
	if !contains(st.Reason, "Caution") {
		t.Errorf("Stage 2 with RS55 < 0 should carry a caution: %s", st.Reason)
	}
	if _, ok := WeinsteinStage(series(33, func(int) float64 { return 1 }), 0, cfg); ok {
		t.Error("33 weeks should be too short")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
