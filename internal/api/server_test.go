package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stockscanner/internal/indicators"
	"stockscanner/internal/provider"
	"stockscanner/internal/scanner"
	"stockscanner/internal/symbols"
)

// fakeProvider generates five years of weekday bars for every ticker it knows.
type fakeProvider struct {
	growth map[string]float64 // daily growth factor per ticker
	short  map[string]int     // tickers with only this many bars
	gap    map[string]bool    // tickers missing their second-to-last session
	calls  int
	icalls int // Intraday calls
}

// Intraday returns three 5-minute bars on the last daily date at 98%, 99% and
// 100% of the last close (equal volume, so VWAP = 99% of the close), preceded
// by a heavy bar from the previous day that must not count.
func (f *fakeProvider) Intraday(ctx context.Context, tickers []string) (map[string]provider.Series, map[string]error) {
	f.icalls++
	daily, errs := f.History(ctx, tickers)
	f.calls--
	out := map[string]provider.Series{}
	for t, s := range daily {
		last := s.Bars[len(s.Bars)-1]
		bar := func(day time.Time, hhmm int, c, v float64) indicators.Bar {
			at := day.Add(time.Duration(hhmm/100)*time.Hour + time.Duration(hhmm%100)*time.Minute)
			return indicators.Bar{Date: at, Open: c, High: c, Low: c, Close: c, Volume: v}
		}
		out[t] = provider.Series{Ticker: t, Bars: []indicators.Bar{
			bar(last.Date.AddDate(0, 0, -1), 1525, last.Close*0.5, 1e9),
			bar(last.Date, 915, last.Close*0.98, 100),
			bar(last.Date, 920, last.Close*0.99, 100),
			bar(last.Date, 925, last.Close, 100),
		}}
	}
	return out, errs
}

// historyOnly hides Intraday, like a price source without intraday data.
type historyOnly struct{ p *fakeProvider }

func (h historyOnly) History(ctx context.Context, tickers []string) (map[string]provider.Series, map[string]error) {
	return h.p.History(ctx, tickers)
}

func (f *fakeProvider) History(_ context.Context, tickers []string) (map[string]provider.Series, map[string]error) {
	f.calls++
	out := map[string]provider.Series{}
	errs := map[string]error{}
	for _, t := range tickers {
		g, ok := f.growth[t]
		if !ok {
			errs[t] = errors.New("No data found, symbol may be delisted")
			continue
		}
		var bars []indicators.Bar
		d := time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
		for i := 0; !d.After(end); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			c := 100 * math.Pow(g, float64(i)) * (1 + 0.01*math.Sin(float64(i)))
			bars = append(bars, indicators.Bar{Date: d, Open: c, High: c * 1.01, Low: c * 0.99, Close: c, Volume: 1000})
			i++
		}
		if n, ok := f.short[t]; ok {
			bars = bars[len(bars)-n:]
		}
		if f.gap[t] {
			bars = append(bars[:len(bars)-2], bars[len(bars)-1])
		}
		out[t] = provider.Series{Ticker: t, Name: t + " Ltd", Bars: bars}
	}
	return out, errs
}

type fakeSymbols struct{ list *symbols.List }

func (f fakeSymbols) Get(context.Context) (*symbols.List, error) {
	if f.list == nil {
		return nil, errors.New("offline")
	}
	return f.list, nil
}

func newTestServer(p provider.Provider, list *symbols.List) http.Handler {
	return (&Server{Scanner: &scanner.Scanner{
		Provider: p,
		Symbols:  fakeSymbols{list},
		Config:   scanner.DefaultConfig(),
	}}).Handler()
}

func testList() *symbols.List {
	return symbols.NewList([]symbols.Equity{
		{Symbol: "TCS", Name: "Tata Consultancy Services Limited", ISIN: "INE467B01029"},
		{Symbol: "TATAMOTORS", Name: "Tata Motors Limited", ISIN: "INE155A01022"},
		{Symbol: "NEWCO", Name: "New Listing Limited", ISIN: "INE000N01010"},
		{Symbol: "GONE", Name: "Delisted Company Limited", ISIN: "INE000G01010"},
	})
}

func post(t *testing.T, h http.Handler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return rec, out
}

func TestAnalyze(t *testing.T) {
	p := &fakeProvider{
		growth: map[string]float64{"^NSEI": 1.0004, "TCS.NS": 1.0008, "TATAMOTORS.NS": 0.9997, "NEWCO.NS": 1.001},
		short:  map[string]int{"NEWCO.NS": 30},
		gap:    map[string]bool{"TATAMOTORS.NS": true},
	}
	h := newTestServer(p, testList())
	rec, out := post(t, h, `{"symbols": "TCS, tcs; Tata Motors\nINE000N01010\nGONE\nNo Such Company Xyz"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %v", rec.Code, out)
	}

	rows := out["rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (TCS, TATAMOTORS, NEWCO): %v", len(rows), out)
	}
	tcs := rows[0].(map[string]any)
	if tcs["symbol"] != "TCS" || tcs["name"] != "Tata Consultancy Services Limited" {
		t.Errorf("first row = %v", tcs)
	}
	for _, k := range []string{"close", "rs55", "rs55_5d_ago", "rsi_d", "rsi_w", "rsi_m", "sma200", "sma20_dist", "pivot_w_dist"} {
		if _, ok := tcs[k].(float64); !ok {
			t.Errorf("TCS %s = %v, want a number", k, tcs[k])
		}
	}
	if rs := tcs["rs55"].(float64); rs <= 0 {
		t.Errorf("TCS grows faster than the index, RS55 = %v", rs)
	}
	if st := tcs["stage"].(map[string]any); st["number"].(float64) != 2 {
		t.Errorf("TCS stage = %v, want 2", st)
	}
	pv := tcs["pivot_d"].(map[string]any)
	for _, k := range []string{"p", "r1", "r2", "r3", "s1", "s2", "s3", "from"} {
		if pv[k] == nil {
			t.Errorf("pivot_d.%s missing", k)
		}
	}

	if w := tcs["warnings"].([]any); len(w) != 0 {
		t.Errorf("TCS has full history, warnings = %v", w)
	}
	// VWAP = 99% of the close, so price is 1/0.99 − 1 = +1.0101% above it.
	if d, ok := tcs["vwap_dist"].(float64); !ok || math.Abs(d-1.0101) > 1e-3 {
		t.Errorf("TCS vwap_dist = %v, want +1.0101", tcs["vwap_dist"])
	}
	if v, ok := tcs["vwap"].(float64); !ok || math.Abs(v-tcs["close"].(float64)*0.99) > 1e-3 {
		t.Errorf("TCS vwap = %v, want 99%% of close %v", tcs["vwap"], tcs["close"])
	}
	if tcs["vwap_date"] != "2026-10-12" {
		t.Errorf("TCS vwap_date = %v", tcs["vwap_date"])
	}
	tm := rows[1].(map[string]any)
	if w := fmt.Sprint(tm["warnings"]); !strings.Contains(w, "2026-10-09") {
		t.Errorf("TATAMOTORS is missing 2026-10-09; warnings = %s", w)
	}

	newco := rows[2].(map[string]any)
	if newco["sma200"] != nil || newco["stage"] != nil || newco["rsi_m"] != nil {
		t.Errorf("short history should give nulls: sma200=%v stage=%v rsi_m=%v", newco["sma200"], newco["stage"], newco["rsi_m"])
	}
	if len(newco["warnings"].([]any)) == 0 {
		t.Error("short history should carry warnings")
	}

	problems := out["problems"].([]any)
	if len(problems) != 2 {
		t.Fatalf("problems = %v, want GONE (no data) and the unknown name", problems)
	}
	bench := out["benchmark"].(map[string]any)
	if bench["ticker"] != "^NSEI" || bench["date"] != "2026-10-12" {
		t.Errorf("benchmark = %v", bench)
	}
}

func TestAnalyzeWithoutIntraday(t *testing.T) {
	p := historyOnly{&fakeProvider{growth: map[string]float64{"^NSEI": 1.0004, "TCS.NS": 1.0008}}}
	rec, out := post(t, newTestServer(p, testList()), `{"symbols": "TCS"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %v", rec.Code, out)
	}
	rows := out["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v; a missing VWAP must not skip the stock", rows)
	}
	row := rows[0].(map[string]any)
	if row["vwap"] != nil || row["vwap_dist"] != nil {
		t.Errorf("vwap = %v, vwap_dist = %v; want null", row["vwap"], row["vwap_dist"])
	}
	if w := fmt.Sprint(row["warnings"]); !strings.Contains(w, "VWAP unavailable") {
		t.Errorf("warnings = %s; want a VWAP warning", w)
	}
}

func TestAnalyzeList(t *testing.T) {
	p := &fakeProvider{growth: map[string]float64{"^NSEI": 1.0004, "TCS.NS": 1.0008}}
	rec, out := post(t, newTestServer(p, testList()), `{"symbols": ["TCS.NS"]}`)
	if rec.Code != http.StatusOK || len(out["rows"].([]any)) != 1 {
		t.Fatalf("status %d: %v", rec.Code, out)
	}
}

func TestAnalyzeErrors(t *testing.T) {
	ok := &fakeProvider{growth: map[string]float64{"^NSEI": 1.0004}}
	many := make([]string, 201)
	for i := range many {
		many[i] = fmt.Sprintf("S%d", i)
	}
	cases := []struct {
		name string
		p    provider.Provider
		body string
		code int
	}{
		{"empty", ok, `{"symbols": "  , ;"}`, http.StatusBadRequest},
		{"too many", ok, `{"symbols": "` + strings.Join(many, ",") + `"}`, http.StatusBadRequest},
		{"bad json", ok, `{"symbols": 5}`, http.StatusBadRequest},
		{"no benchmark", &fakeProvider{growth: map[string]float64{"TCS.NS": 1}}, `{"symbols": "TCS"}`, http.StatusBadGateway},
	}
	for _, c := range cases {
		rec, out := post(t, newTestServer(c.p, testList()), c.body)
		if rec.Code != c.code || out["error"] == nil {
			t.Errorf("%s: status %d body %v, want %d with an error", c.name, rec.Code, out, c.code)
		}
	}
}

func TestSymbolOnlyMode(t *testing.T) {
	p := &fakeProvider{growth: map[string]float64{"^NSEI": 1.0004, "INFY.NS": 1.0002}}
	rec, out := post(t, newTestServer(p, nil), `{"symbols": "INFY, Tata Motors"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if len(out["rows"].([]any)) != 1 || len(out["problems"].([]any)) != 1 || len(out["notices"].([]any)) != 1 {
		t.Errorf("symbol-only mode: %v", out)
	}
}

func TestCachedProvider(t *testing.T) {
	inner := &fakeProvider{growth: map[string]float64{"A": 1, "B": 1}}
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	c := provider.NewCached(inner, 15*time.Minute)
	c.Now = func() time.Time { return now }

	c.History(context.Background(), []string{"A"})
	c.History(context.Background(), []string{"A"})
	if inner.calls != 1 {
		t.Errorf("cached ticker refetched: %d calls", inner.calls)
	}
	s, _ := c.History(context.Background(), []string{"A", "B"})
	if inner.calls != 2 || len(s) != 2 {
		t.Errorf("want one extra call for B: calls=%d series=%d", inner.calls, len(s))
	}
	now = now.Add(16 * time.Minute)
	c.History(context.Background(), []string{"A"})
	if inner.calls != 3 {
		t.Errorf("expired entry not refetched: %d calls", inner.calls)
	}

	// Intraday bars are cached separately, with the same TTL.
	c.Intraday(context.Background(), []string{"A"})
	c.Intraday(context.Background(), []string{"A"})
	if inner.icalls != 1 || inner.calls != 3 {
		t.Errorf("intraday: %d intraday calls, %d history calls; want 1 and 3", inner.icalls, inner.calls)
	}
	now = now.Add(16 * time.Minute)
	if s, _ := c.Intraday(context.Background(), []string{"A"}); inner.icalls != 2 || len(s["A"].Bars) == 0 {
		t.Errorf("expired intraday entry not refetched: %d calls", inner.icalls)
	}

	// A provider without intraday data yields nothing, without errors.
	s, errs := provider.NewCached(historyOnly{inner}, time.Minute).Intraday(context.Background(), []string{"A"})
	if len(s) != 0 || len(errs) != 0 {
		t.Errorf("history-only intraday = %v, %v; want empty", s, errs)
	}
}

func TestSymbols(t *testing.T) {
	get := func(list *symbols.List) (*httptest.ResponseRecorder, map[string]any) {
		rec := httptest.NewRecorder()
		newTestServer(&fakeProvider{}, list).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/symbols", nil))
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}
	rec, out := get(testList())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	syms := out["symbols"].([]any)
	if len(syms) != 4 || syms[0].(map[string]any)["symbol"] != "TCS" || syms[0].(map[string]any)["isin"] != "INE467B01029" {
		t.Errorf("symbols = %v", syms)
	}
	if rec, _ := get(nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without a list: status %d, want 503", rec.Code)
	}
}

func TestParseEntries(t *testing.T) {
	got := scanner.ParseEntries("RELIANCE, tcs;TCS\n  Tata   Motors \r\n,,INE155A01022")
	want := []string{"RELIANCE", "tcs", "Tata Motors", "INE155A01022"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("ParseEntries = %q, want %q", got, want)
	}
}
