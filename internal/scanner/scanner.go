// Package scanner turns a list of entries into indicator rows: it resolves
// symbols, fetches prices through a Provider and runs the indicator engine.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"stockscanner/internal/fundamentals"
	"stockscanner/internal/indicators"
	"stockscanner/internal/provider"
	"stockscanner/internal/symbols"
)

// SymbolSource supplies the NSE equity list; an error means symbol-only mode.
type SymbolSource interface {
	Get(ctx context.Context) (*symbols.List, error)
}

// FundamentalsSource supplies cash flow and shareholding data by Yahoo ticker.
// It never fails as a whole; per-stock problems are in each Data's Notes.
type FundamentalsSource interface {
	Get(ctx context.Context, tickers []string) map[string]fundamentals.Data
}

// fundamentalsTimeout caps how long a scan waits for fundamentals, so a slow
// filing source never holds up the price columns for long.
const fundamentalsTimeout = 45 * time.Second

// Config holds indicator parameters.
type Config struct {
	Benchmark     string // Yahoo ticker of the benchmark, ^NSEI
	BenchmarkName string
	RSIPeriod     int // 14
	RSPeriod      int // 55
	RSBack        int // 5
	MinRSIMonths  int // 40: fewer monthly bars carry a warning
	Stage         indicators.StageConfig
}

// DefaultConfig returns the design-doc parameters.
func DefaultConfig() Config {
	return Config{
		Benchmark:     "^NSEI",
		BenchmarkName: "Nifty 50",
		RSIPeriod:     14,
		RSPeriod:      55,
		RSBack:        5,
		MinRSIMonths:  40,
		Stage:         indicators.DefaultStageConfig(),
	}
}

// Scanner runs scans.
type Scanner struct {
	Provider provider.Provider
	Symbols  SymbolSource
	// Fundamentals is optional; without it the fundamentals columns are empty.
	Fundamentals FundamentalsSource
	Config       Config
	Now          func() time.Time
}

// InputError is a problem with the request itself (HTTP 400).
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

// ErrBenchmark means the benchmark could not be downloaded (HTTP 502).
var ErrBenchmark = errors.New("benchmark unavailable")

// Result is the API response body.
type Result struct {
	Rows        []Row     `json:"rows"`
	Problems    []Problem `json:"problems"`
	Benchmark   Benchmark `json:"benchmark"`
	Notices     []string  `json:"notices"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Problem is a skipped entry.
type Problem struct {
	Query  string `json:"query"`
	Reason string `json:"reason"`
}

// Benchmark describes the index every stock is compared with.
type Benchmark struct {
	Ticker string `json:"ticker"`
	Name   string `json:"name"`
	Close  Num    `json:"close"`
	Date   string `json:"date"`
}

// PivotLevels is a pivot in force and the date of its source bar.
type PivotLevels struct {
	P    Num    `json:"p"`
	R1   Num    `json:"r1"`
	R2   Num    `json:"r2"`
	R3   Num    `json:"r3"`
	S1   Num    `json:"s1"`
	S2   Num    `json:"s2"`
	S3   Num    `json:"s3"`
	From string `json:"from"`
}

// Row is one stock's indicators.
type Row struct {
	Query      string `json:"query"`
	Symbol     string `json:"symbol"`
	Ticker     string `json:"ticker"`
	Name       string `json:"name"`
	Date       string `json:"date"`
	Close      Num    `json:"close"`
	ChangePct  Num    `json:"change_pct"`
	RS55       Num    `json:"rs55"`
	RS55Ago    Num    `json:"rs55_5d_ago"`
	RSID       Num    `json:"rsi_d"`
	RSIW       Num    `json:"rsi_w"`
	RSIM       Num    `json:"rsi_m"`
	SMA9       Num    `json:"sma9"`
	SMA20      Num    `json:"sma20"`
	SMA50      Num    `json:"sma50"`
	SMA200     Num    `json:"sma200"`
	SMA9Dist   Num    `json:"sma9_dist"`
	SMA20Dist  Num    `json:"sma20_dist"`
	SMA50Dist  Num    `json:"sma50_dist"`
	SMA200Dist Num    `json:"sma200_dist"`

	PivotD     *PivotLevels `json:"pivot_d"`
	PivotW     *PivotLevels `json:"pivot_w"`
	PivotM     *PivotLevels `json:"pivot_m"`
	PivotDDist Num          `json:"pivot_d_dist"`
	PivotWDist Num          `json:"pivot_w_dist"`
	PivotMDist Num          `json:"pivot_m_dist"`

	VWAP     Num    `json:"vwap"`
	VWAPDist Num    `json:"vwap_dist"`
	VWAPDate string `json:"vwap_date"`

	Stage *indicators.Stage `json:"stage"`

	// Annual cash flows in ₹ crore for the financial year ending …_fy, and the year before.
	FCF         Num    `json:"fcf"`
	FCFPrev     Num    `json:"fcf_prev"`
	FCFFY       string `json:"fcf_fy"`
	NetCash     Num    `json:"net_cash"`
	NetCashPrev Num    `json:"net_cash_prev"`
	NetCashFY   string `json:"net_cash_fy"`

	// FII and DII holdings in % at the latest quarter end and the one before;
	// …Chg is the change in percentage points.
	FII        Num    `json:"fii"`
	FIIPrev    Num    `json:"fii_prev"`
	FIIChg     Num    `json:"fii_chg"`
	DII        Num    `json:"dii"`
	DIIPrev    Num    `json:"dii_prev"`
	DIIChg     Num    `json:"dii_chg"`
	SHPQuarter string `json:"shp_quarter"`
	SHPPrev    string `json:"shp_prev_quarter"`

	Warnings   []string `json:"warnings"`
	Note       string   `json:"note"`
	Method     string   `json:"match"`
	Candidates []string `json:"candidates"`
}

const dateFmt = "2006-01-02"

// Analyze scans entries. It fails only for bad input or a missing benchmark;
// any per-stock failure becomes a Problem.
func (s *Scanner) Analyze(ctx context.Context, entries []string) (*Result, error) {
	if len(entries) == 0 {
		return nil, &InputError{"Enter at least one stock symbol, ISIN or company name."}
	}
	if len(entries) > MaxEntries {
		return nil, &InputError{fmt.Sprintf("Too many entries: %d (the limit is %d).", len(entries), MaxEntries)}
	}
	cfg := s.Config
	res := &Result{Rows: []Row{}, Problems: []Problem{}, Notices: []string{}}

	var resolver symbols.Resolver
	if s.Symbols != nil {
		list, err := s.Symbols.Get(ctx)
		if err != nil {
			res.Notices = append(res.Notices, "NSE symbol list unavailable: only symbols and Yahoo tickers can be resolved.")
		} else {
			resolver.List = list
			if list.Stale {
				res.Notices = append(res.Notices, fmt.Sprintf("NSE symbol list refresh failed; using the copy from %s.", list.FetchedAt.Format(dateFmt)))
			}
		}
	}

	var matches []symbols.Match
	seen := map[string]string{}
	for _, e := range entries {
		m, err := resolver.Resolve(e)
		if err != nil {
			res.Problems = append(res.Problems, Problem{e, err.Error()})
			continue
		}
		if first, dup := seen[m.Ticker]; dup {
			res.Problems = append(res.Problems, Problem{e, fmt.Sprintf("Duplicate of %q (%s)", first, m.Ticker)})
			continue
		}
		seen[m.Ticker] = e
		matches = append(matches, m)
	}

	tickers := []string{cfg.Benchmark}
	for _, m := range matches {
		if m.Ticker != cfg.Benchmark {
			tickers = append(tickers, m.Ticker)
		}
	}
	// Fundamentals download alongside prices.
	fundCh := make(chan map[string]fundamentals.Data, 1)
	if s.Fundamentals != nil && len(matches) > 0 {
		stockTickers := make([]string, len(matches))
		for i, m := range matches {
			stockTickers[i] = m.Ticker
		}
		go func() {
			fctx, cancel := context.WithTimeout(ctx, fundamentalsTimeout)
			defer cancel()
			fundCh <- s.Fundamentals.Get(fctx, stockTickers)
		}()
	} else {
		fundCh <- nil
	}

	series, errs := s.Provider.History(ctx, tickers)
	bench, ok := series[cfg.Benchmark]
	if !ok || len(bench.Bars) == 0 {
		reason := "no data"
		if err := errs[cfg.Benchmark]; err != nil {
			reason = err.Error()
		}
		return nil, fmt.Errorf("%w: could not download %s (%s): %s", ErrBenchmark, cfg.BenchmarkName, cfg.Benchmark, reason)
	}
	lastBench := bench.Bars[len(bench.Bars)-1]
	res.Benchmark = Benchmark{
		Ticker: cfg.Benchmark,
		Name:   cfg.BenchmarkName,
		Close:  Num(lastBench.Close),
		Date:   lastBench.Date.Format(dateFmt),
	}

	// Intraday bars are only needed for the VWAP column; failures there never skip a stock.
	ip, hasIntraday := s.Provider.(provider.IntradayProvider)
	var intra map[string]provider.Series
	var intraErrs map[string]error
	if hasIntraday && len(matches) > 0 {
		stockTickers := make([]string, len(matches))
		for i, m := range matches {
			stockTickers[i] = m.Ticker
		}
		intra, intraErrs = ip.Intraday(ctx, stockTickers)
	}
	fund := <-fundCh

	for _, m := range matches {
		sr, ok := series[m.Ticker]
		if !ok {
			reason := "no price data"
			if err := errs[m.Ticker]; err != nil {
				reason = err.Error()
			}
			res.Problems = append(res.Problems, Problem{m.Query, fmt.Sprintf("%s: %s", m.Ticker, reason)})
			continue
		}
		in := intraday{supported: hasIntraday, series: intra[m.Ticker], err: intraErrs[m.Ticker]}
		row, err := s.safeRow(m, sr, bench, in)
		if err != nil {
			res.Problems = append(res.Problems, Problem{m.Query, err.Error()})
			continue
		}
		if fund != nil {
			applyFundamentals(&row, fund[m.Ticker])
		}
		res.Rows = append(res.Rows, row)
	}

	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	res.GeneratedAt = now().UTC()
	return res, nil
}

// applyFundamentals copies one stock's fundamentals into its row.
func applyFundamentals(row *Row, d fundamentals.Data) {
	row.FCF, row.FCFPrev, row.FCFFY = Num(d.FCF), Num(d.FCFPrev), d.FCFYear
	row.NetCash, row.NetCashPrev, row.NetCashFY = Num(d.NetCash), Num(d.NetCashPrev), d.NetCashYear
	row.FII, row.FIIPrev, row.FIIChg = Num(d.FII), Num(d.FIIPrev), Num(d.FII-d.FIIPrev)
	row.DII, row.DIIPrev, row.DIIChg = Num(d.DII), Num(d.DIIPrev), Num(d.DII-d.DIIPrev)
	row.SHPQuarter, row.SHPPrev = d.Quarter, d.PrevQuarter
	row.Warnings = append(row.Warnings, d.Notes...)
}

// missingSessions lists benchmark dates among the last `window` sessions that
// the stock has no bar for (only dates after the stock's first bar count).
func missingSessions(stock, bench []indicators.Bar, window int) []string {
	have := make(map[time.Time]bool, window)
	for _, b := range stock[max(0, len(stock)-window*2):] {
		have[b.Date] = true
	}
	var out []string
	for _, b := range bench[max(0, len(bench)-window):] {
		if b.Date.After(stock[0].Date) && !have[b.Date] {
			out = append(out, b.Date.Format(dateFmt))
		}
	}
	return out
}

// intraday is one stock's intraday fetch result; supported is false when the
// provider has no intraday data at all.
type intraday struct {
	supported bool
	series    provider.Series
	err       error
}

// lastSession returns the intraday bars from the latest calendar date.
func lastSession(bars []indicators.Bar) []indicators.Bar {
	if len(bars) == 0 {
		return nil
	}
	y, m, d := bars[len(bars)-1].Date.Date()
	i := len(bars)
	for i > 0 {
		if by, bm, bd := bars[i-1].Date.Date(); by != y || bm != m || bd != d {
			break
		}
		i--
	}
	return bars[i:]
}

// safeRow keeps one bad calculation from failing the whole request.
func (s *Scanner) safeRow(m symbols.Match, sr provider.Series, bench provider.Series, in intraday) (row Row, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("calculation failed: %v", r)
		}
	}()
	return s.computeRow(m, sr, bench, in)
}

func (s *Scanner) computeRow(m symbols.Match, sr provider.Series, bench provider.Series, in intraday) (Row, error) {
	cfg := s.Config
	daily := sr.Bars
	if len(daily) < 2 {
		return Row{}, fmt.Errorf("%s: not enough price history (%d bars)", m.Ticker, len(daily))
	}
	closes := indicators.Closes(daily)
	last := daily[len(daily)-1]
	price := last.Close

	name := m.Name
	if name == "" {
		name = sr.Name
	}
	row := Row{
		Query:      m.Query,
		Symbol:     m.Symbol,
		Ticker:     m.Ticker,
		Name:       name,
		Date:       last.Date.Format(dateFmt),
		Close:      Num(price),
		ChangePct:  Num(indicators.PctDist(price, closes[len(closes)-2])),
		Note:       m.Note,
		Method:     m.Method,
		Candidates: m.Candidates,
		Warnings:   []string{},
	}
	nan := Num(math.NaN())
	row.FCF, row.FCFPrev, row.NetCash, row.NetCashPrev = nan, nan, nan, nan
	row.FII, row.FIIPrev, row.FIIChg, row.DII, row.DIIPrev, row.DIIChg = nan, nan, nan, nan, nan, nan
	if row.Candidates == nil {
		row.Candidates = []string{}
	}
	warn := func(format string, a ...any) { row.Warnings = append(row.Warnings, fmt.Sprintf(format, a...)) }

	benchLast := bench.Bars[len(bench.Bars)-1].Date
	if !last.Date.Equal(benchLast) {
		warn("Last price is from %s; %s data is from %s.", row.Date, cfg.BenchmarkName, benchLast.Format(dateFmt))
	}
	if gaps := missingSessions(daily, bench.Bars, 10); len(gaps) > 0 {
		warn("No price data for %s, when the %s traded; day change and daily values skip it.", strings.Join(gaps, ", "), cfg.BenchmarkName)
	}

	rs, rsAgo := indicators.RelativeStrength(daily, bench.Bars, cfg.RSPeriod, cfg.RSBack)
	row.RS55, row.RS55Ago = Num(rs), Num(rsAgo)
	if math.IsNaN(rs) {
		warn("RS55 needs %d sessions shared with the %s.", cfg.RSPeriod+1, cfg.BenchmarkName)
	}

	weekly := indicators.Weekly(daily)
	monthly := indicators.Monthly(daily)
	row.RSID = Num(indicators.Last(indicators.RSI(closes, cfg.RSIPeriod)))
	row.RSIW = Num(indicators.Last(indicators.RSI(indicators.Closes(weekly), cfg.RSIPeriod)))
	row.RSIM = Num(indicators.Last(indicators.RSI(indicators.Closes(monthly), cfg.RSIPeriod)))
	for _, r := range []struct {
		v     Num
		label string
		have  int
		unit  string
	}{{row.RSID, "Daily", len(daily), "days"}, {row.RSIW, "Weekly", len(weekly), "weeks"}, {row.RSIM, "Monthly", len(monthly), "months"}} {
		if math.IsNaN(float64(r.v)) {
			warn("%s RSI needs %d bars; only %d %s of history.", r.label, cfg.RSIPeriod+1, r.have, r.unit)
		}
	}
	if !math.IsNaN(float64(row.RSIM)) && len(monthly) < cfg.MinRSIMonths {
		warn("Monthly RSI is based on only %d months (%d+ recommended); it may differ from your charting tool.", len(monthly), cfg.MinRSIMonths)
	}

	smas := []struct {
		n         int
		val, dist *Num
	}{{9, &row.SMA9, &row.SMA9Dist}, {20, &row.SMA20, &row.SMA20Dist}, {50, &row.SMA50, &row.SMA50Dist}, {200, &row.SMA200, &row.SMA200Dist}}
	for _, x := range smas {
		v := indicators.SMA(closes, x.n)
		*x.val, *x.dist = Num(v), Num(indicators.PctDist(price, v))
		if math.IsNaN(v) {
			warn("SMA %d needs %d sessions; only %d available.", x.n, x.n, len(daily))
		}
	}

	for _, p := range []struct {
		bars  []indicators.Bar
		out   **PivotLevels
		dist  *Num
		label string
	}{{daily, &row.PivotD, &row.PivotDDist, "daily"}, {weekly, &row.PivotW, &row.PivotWDist, "weekly"}, {monthly, &row.PivotM, &row.PivotMDist, "monthly"}} {
		pv, ok := indicators.PivotInForce(p.bars)
		*p.dist = Num(math.NaN())
		if !ok {
			warn("No completed %s bar for a pivot.", p.label)
			continue
		}
		*p.out = &PivotLevels{
			P: Num(pv.P), R1: Num(pv.R1), R2: Num(pv.R2), R3: Num(pv.R3),
			S1: Num(pv.S1), S2: Num(pv.S2), S3: Num(pv.S3),
			From: pv.From.Format(dateFmt),
		}
		*p.dist = Num(indicators.PctDist(price, pv.P))
	}

	row.VWAP, row.VWAPDist = Num(math.NaN()), Num(math.NaN())
	session := lastSession(in.series.Bars)
	vwap := indicators.VWAP(session)
	switch {
	case !in.supported:
		warn("Daily VWAP unavailable: the price source has no intraday data.")
	case in.err != nil:
		warn("Daily VWAP unavailable: %v.", in.err)
	case math.IsNaN(vwap):
		warn("Daily VWAP unavailable: no intraday volume.")
	default:
		row.VWAP, row.VWAPDist = Num(vwap), Num(indicators.PctDist(price, vwap))
		row.VWAPDate = session[0].Date.Format(dateFmt)
		if row.VWAPDate != row.Date {
			warn("Daily VWAP is from the %s session; the last daily price is from %s.", row.VWAPDate, row.Date)
		}
	}

	if st, ok := indicators.WeinsteinStage(indicators.Closes(weekly), rs, cfg.Stage); ok {
		row.Stage = &st
	} else {
		warn("Stage needs %d weeks of history; only %d available.", cfg.Stage.MinWeeks(), len(weekly))
	}

	if strings.TrimSpace(row.Name) == "" {
		row.Name = row.Symbol
	}
	return row, nil
}
