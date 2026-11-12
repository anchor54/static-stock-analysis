// Package fundamentals fetches slow-moving company data for the scanner:
// annual free cash flow and net change in cash from Yahoo's fundamentals
// time series, and quarterly FII/DII holdings from the shareholding-pattern
// XBRL filings NSE publishes.
package fundamentals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Data is one stock's fundamentals. Missing numbers are NaN; Notes explains
// any part that could not be fetched.
type Data struct {
	// Annual free cash flow and net change in cash, in ₹ crore, for the
	// latest financial year (…Year, the year-end date) and the one before.
	FCF, FCFPrev         float64
	FCFYear              string
	NetCash, NetCashPrev float64
	NetCashYear          string
	// CashNote explains a currency conversion, e.g. for a company Yahoo
	// reports in US dollars.
	CashNote string

	// Holdings in % of total shares at the latest quarter end (Quarter) and
	// the quarter before (PrevQuarter). FII is the filing's "Institutions
	// (Foreign)" total (FPIs and FDI); DII is "Institutions (Domestic)"
	// (mutual funds, insurers, banks, pension funds and others).
	FII, FIIPrev, DII, DIIPrev float64
	Quarter, PrevQuarter       string
	// HoldingSource says where the holdings came from: "NSE" (live) or
	// "snapshot of YYYY-MM-DD" (the saved file, used when NSE is unreachable).
	HoldingSource string

	Notes []string
}

func empty() Data {
	n := math.NaN()
	return Data{FCF: n, FCFPrev: n, NetCash: n, NetCashPrev: n, FII: n, FIIPrev: n, DII: n, DIIPrev: n}
}

// Source fetches and caches fundamentals.
type Source struct {
	Client      *http.Client
	YahooURL    string        // default https://query1.finance.yahoo.com
	NSEURL      string        // default https://www.nseindia.com
	Concurrency int           // stocks fetched at once, default 6
	TTL         time.Duration // how long a complete result is reused, default 12h
	Now         func() time.Time
	// Snapshot holds saved holdings, used when NSE can't be reached (it blocks
	// most cloud servers). Optional.
	Snapshot *Snapshot

	mu    sync.Mutex
	cache map[string]entry
	// Parsed filings by URL. NSE gives each filing (and each revision) its own
	// URL, so these never go stale.
	filings map[string]holding
	// While NSE is refusing requests, go straight to the snapshot.
	nseBlockedUntil time.Time
	fx              map[string]fxEntry

	cookieMu  sync.Mutex
	cookieSet time.Time
}

type entry struct {
	data    Data
	expires time.Time
}

type holding struct{ fii, dii float64 }

type fxEntry struct {
	rate    float64
	expires time.Time
}

// Holding is one stock's FII and DII holdings, in % of total shares, at the
// latest quarter end and the quarter before (dates as YYYY-MM-DD). Missing
// values are NaN.
type Holding struct {
	Quarter, PrevQuarter       string
	FII, DII, FIIPrev, DIIPrev float64
}

// errBlocked means NSE refused the request, as it does for most cloud servers.
var errBlocked = errors.New("NSE refused the request (HTTP 403)")

// nseBlockPause is how long to skip NSE after it refuses a request.
const nseBlockPause = 30 * time.Minute

// New returns a Source with default settings.
func New() *Source {
	jar, _ := cookiejar.New(nil)
	return &Source{
		Client:      &http.Client{Timeout: 20 * time.Second, Jar: jar},
		YahooURL:    "https://query1.finance.yahoo.com",
		NSEURL:      "https://www.nseindia.com",
		Concurrency: 6,
		TTL:         12 * time.Hour,
	}
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36"

// incompleteTTL is how long a result with a failed part is reused, so a
// temporary outage is retried soon without hammering the source on every scan.
const incompleteTTL = 10 * time.Minute

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Get returns fundamentals for each Yahoo ticker. It never fails as a whole;
// problems are reported in each stock's Notes.
func (s *Source) Get(ctx context.Context, tickers []string) map[string]Data {
	out := make(map[string]Data, len(tickers))
	var missing []string
	now := s.now()
	s.mu.Lock()
	for _, t := range tickers {
		if e, ok := s.cache[t]; ok && now.Before(e.expires) {
			out[t] = e.data
		} else {
			missing = append(missing, t)
		}
	}
	s.mu.Unlock()

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(s.Concurrency, 1))
	for _, t := range missing {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				d := empty()
				d.Notes = []string{"Fundamentals skipped: the request timed out."}
				out[t] = d
				mu.Unlock()
				return
			}
			defer func() { <-sem }()
			d := s.fetch(ctx, t)
			ttl := s.TTL
			if ttl == 0 {
				ttl = 12 * time.Hour
			}
			if len(d.Notes) > 0 {
				ttl = min(ttl, incompleteTTL)
			}
			mu.Lock()
			out[t] = d
			mu.Unlock()
			s.mu.Lock()
			if s.cache == nil {
				s.cache = make(map[string]entry)
			}
			s.cache[t] = entry{data: d, expires: s.now().Add(ttl)}
			s.mu.Unlock()
		}(t)
	}
	wg.Wait()
	return out
}

func (s *Source) fetch(ctx context.Context, ticker string) Data {
	d := empty()
	if err := s.cashFlow(ctx, ticker, &d); err != nil {
		d.Notes = append(d.Notes, fmt.Sprintf("Cash flow unavailable: %v.", err))
	}
	if sym, ok := strings.CutSuffix(strings.ToUpper(ticker), ".NS"); ok {
		s.holdings(ctx, sym, &d)
	} else {
		d.Notes = append(d.Notes, "FII/DII holding is only available for NSE-listed stocks.")
	}
	return d
}

// get downloads a URL with retries on network errors, 429 and 5xx.
func (s *Source) get(ctx context.Context, u string, header map[string]string) ([]byte, int, error) {
	var body []byte
	var status int
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := s.Client.Do(req)
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			resp.Body.Close()
			status = resp.StatusCode
		}
		retryable := err != nil || status == http.StatusTooManyRequests || status >= 500
		if !retryable || attempt == 2 {
			if err != nil {
				return nil, 0, fmt.Errorf("download failed: %w", err)
			}
			return body, status, nil
		}
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
}

// ---- Yahoo: annual free cash flow and net change in cash ----

const crore = 1e7

func (s *Source) cashFlow(ctx context.Context, ticker string, d *Data) error {
	now := s.now()
	u := fmt.Sprintf("%s/ws/fundamentals-timeseries/v1/finance/timeseries/%s?symbol=%s&type=annualFreeCashFlow,annualChangesInCash&period1=%d&period2=%d",
		s.YahooURL, url.PathEscape(ticker), url.QueryEscape(ticker), now.AddDate(-6, 0, 0).Unix(), now.AddDate(0, 0, 1).Unix())
	body, status, err := s.get(ctx, u, map[string]string{"Accept": "application/json"})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("Yahoo returned HTTP %d", status)
	}
	series, err := parseTimeseries(body)
	if err != nil {
		return err
	}
	fcf, cash := series["annualFreeCashFlow"], series["annualChangesInCash"]
	if len(fcf) == 0 && len(cash) == 0 {
		return errors.New("Yahoo has no cash flow statement for this stock")
	}
	d.FCF, d.FCFPrev, d.FCFYear = lastTwo(fcf)
	d.NetCash, d.NetCashPrev, d.NetCashYear = lastTwo(cash)

	// A few companies (e.g. Infosys) report to Yahoo in US dollars: convert to rupees.
	cur := currencyOf(fcf)
	if cur == "" {
		cur = currencyOf(cash)
	}
	if cur != "" && cur != "INR" {
		rate, err := s.fxRate(ctx, cur)
		if err != nil {
			nan := math.NaN()
			d.FCF, d.FCFPrev, d.NetCash, d.NetCashPrev = nan, nan, nan, nan
			return fmt.Errorf("reported in %s and the %s/INR rate is unavailable (%v)", cur, cur, err)
		}
		d.FCF, d.FCFPrev, d.NetCash, d.NetCashPrev = d.FCF*rate, d.FCFPrev*rate, d.NetCash*rate, d.NetCashPrev*rate
		d.CashNote = fmt.Sprintf("Reported in %s; converted at today's rate of ₹%.2f per %s", cur, rate, cur)
	}
	return nil
}

// currencyOf returns the currency of the latest point.
func currencyOf(ps []point) string {
	if len(ps) == 0 {
		return ""
	}
	return strings.ToUpper(ps[len(ps)-1].currency)
}

// fxRate returns the latest rupees per unit of cur, from Yahoo's chart API, cached for 12 hours.
func (s *Source) fxRate(ctx context.Context, cur string) (float64, error) {
	s.mu.Lock()
	e, ok := s.fx[cur]
	s.mu.Unlock()
	if ok && s.now().Before(e.expires) {
		return e.rate, nil
	}
	ticker := cur + "INR=X"
	body, status, err := s.get(ctx, fmt.Sprintf("%s/v8/finance/chart/%s?range=5d&interval=1d", s.YahooURL, url.PathEscape(ticker)),
		map[string]string{"Accept": "application/json"})
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("Yahoo returned HTTP %d for %s", status, ticker)
	}
	var cr struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Price float64 `json:"regularMarketPrice"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &cr); err != nil || len(cr.Chart.Result) == 0 || cr.Chart.Result[0].Meta.Price <= 0 {
		return 0, fmt.Errorf("no %s rate from Yahoo", ticker)
	}
	rate := cr.Chart.Result[0].Meta.Price
	s.mu.Lock()
	if s.fx == nil {
		s.fx = make(map[string]fxEntry)
	}
	s.fx[cur] = fxEntry{rate: rate, expires: s.now().Add(12 * time.Hour)}
	s.mu.Unlock()
	return rate, nil
}

type point struct {
	date     string // YYYY-MM-DD
	value    float64
	currency string
}

// lastTwo returns the latest value and the one before, in ₹ crore.
func lastTwo(ps []point) (last, prev float64, date string) {
	last, prev = math.NaN(), math.NaN()
	if n := len(ps); n > 0 {
		last, date = ps[n-1].value/crore, ps[n-1].date
		if n > 1 {
			prev = ps[n-2].value / crore
		}
	}
	return last, prev, date
}

// parseTimeseries reads Yahoo's fundamentals-timeseries response into
// type → points, oldest first. Each result holds its points under a key named
// after its type, and years with no data are null.
func parseTimeseries(body []byte) (map[string][]point, error) {
	var resp struct {
		Timeseries struct {
			Result []map[string]json.RawMessage `json:"result"`
			Error  *struct {
				Description string `json:"description"`
			} `json:"error"`
		} `json:"timeseries"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.New("Yahoo returned an unreadable response")
	}
	if e := resp.Timeseries.Error; e != nil {
		return nil, fmt.Errorf("Yahoo: %s", e.Description)
	}
	out := map[string][]point{}
	for _, res := range resp.Timeseries.Result {
		var meta struct {
			Type []string `json:"type"`
		}
		if err := json.Unmarshal(res["meta"], &meta); err != nil || len(meta.Type) == 0 {
			continue
		}
		typ := meta.Type[0]
		var raw []*struct {
			AsOfDate      string `json:"asOfDate"`
			CurrencyCode  string `json:"currencyCode"`
			ReportedValue struct {
				Raw *float64 `json:"raw"`
			} `json:"reportedValue"`
		}
		if v, ok := res[typ]; !ok || json.Unmarshal(v, &raw) != nil {
			continue
		}
		var ps []point
		for _, p := range raw {
			if p != nil && p.ReportedValue.Raw != nil && p.AsOfDate != "" {
				ps = append(ps, point{p.AsOfDate, *p.ReportedValue.Raw, p.CurrencyCode})
			}
		}
		sort.Slice(ps, func(i, j int) bool { return ps[i].date < ps[j].date })
		out[typ] = ps
	}
	return out, nil
}

// ---- NSE: quarterly shareholding pattern ----

// Filing is one quarter's shareholding-pattern submission in NSE's list.
type Filing struct {
	Date      string `json:"date"`          // quarter end, e.g. 30-SEP-2026
	Broadcast string `json:"broadcastDate"` // e.g. 21-OCT-2026 16:25:15
	XBRL      string `json:"xbrl"`
}

// holdings fills d's FII/DII fields: live from NSE when it answers, else from
// the snapshot.
func (s *Source) holdings(ctx context.Context, symbol string, d *Data) {
	s.mu.Lock()
	blocked := s.now().Before(s.nseBlockedUntil)
	s.mu.Unlock()

	var err error
	if !blocked {
		var h Holding
		if h, err = s.Holding(ctx, symbol); err == nil {
			setHolding(d, h, "NSE")
			return
		}
		if errors.Is(err, errBlocked) {
			s.mu.Lock()
			s.nseBlockedUntil = s.now().Add(nseBlockPause)
			s.mu.Unlock()
		}
	}
	if h, ok := s.Snapshot.Lookup(symbol); ok {
		setHolding(d, h, "snapshot of "+s.Snapshot.Date())
		return
	}
	if err == nil {
		err = errBlocked
	}
	msg := fmt.Sprintf("FII/DII holding unavailable: %v", err)
	if errors.Is(err, errBlocked) {
		msg += "; this stock is not in the saved snapshot either (run npm run refresh-holdings)"
	}
	d.Notes = append(d.Notes, msg+".")
}

func setHolding(d *Data, h Holding, source string) {
	d.FII, d.DII, d.FIIPrev, d.DIIPrev = h.FII, h.DII, h.FIIPrev, h.DIIPrev
	d.Quarter, d.PrevQuarter, d.HoldingSource = h.Quarter, h.PrevQuarter, source
}

// Holding downloads one NSE symbol's FII and DII holdings for the two latest
// quarters from NSE's shareholding-pattern filings.
func (s *Source) Holding(ctx context.Context, symbol string) (Holding, error) {
	quarters, err := s.Filings(ctx, symbol)
	if err != nil {
		return Holding{FII: math.NaN(), DII: math.NaN(), FIIPrev: math.NaN(), DIIPrev: math.NaN()}, err
	}
	return s.HoldingFrom(ctx, quarters)
}

// HoldingFrom reads the holdings from filings listed by Filings.
func (s *Source) HoldingFrom(ctx context.Context, quarters []Filing) (Holding, error) {
	h := Holding{FII: math.NaN(), DII: math.NaN(), FIIPrev: math.NaN(), DIIPrev: math.NaN()}
	for i, f := range quarters {
		fh, err := s.filing(ctx, f.XBRL)
		if err != nil {
			return h, fmt.Errorf("%s filing: %w", f.Date, err)
		}
		if i == 0 {
			h.FII, h.DII, h.Quarter = fh.fii, fh.dii, quarterDate(f.Date)
		} else {
			h.FIIPrev, h.DIIPrev, h.PrevQuarter = fh.fii, fh.dii, quarterDate(f.Date)
		}
	}
	return h, nil
}

// Filings lists an NSE symbol's two latest shareholding-pattern filings, newest first.
func (s *Source) Filings(ctx context.Context, symbol string) ([]Filing, error) {
	u := fmt.Sprintf("%s/api/corporate-share-holdings-master?index=equities&symbol=%s", s.NSEURL, url.QueryEscape(symbol))
	hdr := map[string]string{"Accept": "application/json", "Referer": s.NSEURL + "/companies-listing/corporate-filings-shareholding-pattern"}
	body, status, err := s.get(ctx, u, hdr)
	if err == nil && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		// NSE sometimes wants its session cookies first: visit the home page, then retry once.
		s.RefreshCookies(ctx)
		body, status, err = s.get(ctx, u, hdr)
	}
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, errBlocked
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("NSE returned HTTP %d", status)
	}
	var list []Filing
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, errors.New("NSE returned an unreadable response")
	}
	quarters := latestQuarters(list, 2)
	if len(quarters) == 0 {
		return nil, errors.New("NSE lists no shareholding filings")
	}
	return quarters, nil
}

// IsBlocked reports whether err means NSE refused the request.
func IsBlocked(err error) bool { return errors.Is(err, errBlocked) }

// RefreshCookies visits NSE's home page for fresh session cookies (at most once a minute).
func (s *Source) RefreshCookies(ctx context.Context) {
	s.cookieMu.Lock()
	defer s.cookieMu.Unlock()
	if s.now().Sub(s.cookieSet) < time.Minute {
		return // another stock just did it
	}
	s.get(ctx, s.NSEURL+"/", map[string]string{"Accept": "text/html"})
	s.cookieSet = s.now()
}

const (
	nseDate      = "02-Jan-2006"
	nseTimestamp = "02-Jan-2006 15:04:05"
)

// latestQuarters returns the newest n distinct quarters with an XBRL file,
// newest first. When a quarter was filed more than once (a revision), the
// latest broadcast wins.
func latestQuarters(list []Filing, n int) []Filing {
	best := map[time.Time]Filing{}
	bestAt := map[time.Time]time.Time{}
	for _, f := range list {
		q, err := time.Parse(nseDate, strings.TrimSpace(f.Date))
		if err != nil || f.XBRL == "" || !strings.HasPrefix(f.XBRL, "http") {
			continue
		}
		at, _ := time.Parse(nseTimestamp, strings.TrimSpace(f.Broadcast))
		if prev, ok := bestAt[q]; !ok || at.After(prev) {
			best[q], bestAt[q] = f, at
		}
	}
	qs := make([]time.Time, 0, len(best))
	for q := range best {
		qs = append(qs, q)
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].After(qs[j]) })
	out := make([]Filing, 0, n)
	for _, q := range qs[:min(n, len(qs))] {
		out = append(out, best[q])
	}
	return out
}

// QuarterDate turns NSE's quarter-end date (30-SEP-2026) into YYYY-MM-DD.
func QuarterDate(s string) string { return quarterDate(s) }

func quarterDate(s string) string {
	if t, err := time.Parse(nseDate, strings.TrimSpace(s)); err == nil {
		return t.Format("2006-01-02")
	}
	return s
}

func (s *Source) filing(ctx context.Context, u string) (holding, error) {
	s.mu.Lock()
	h, ok := s.filings[u]
	s.mu.Unlock()
	if ok {
		return h, nil
	}
	body, status, err := s.get(ctx, u, map[string]string{"Accept": "application/xml"})
	if err != nil {
		return holding{}, err
	}
	if status != http.StatusOK {
		return holding{}, fmt.Errorf("NSE returned HTTP %d", status)
	}
	h, err = parseHolding(body)
	if err != nil {
		return holding{}, err
	}
	s.mu.Lock()
	if s.filings == nil {
		s.filings = make(map[string]holding)
	}
	s.filings[u] = h
	s.mu.Unlock()
	return h, nil
}

// pctFact matches a "% of total shares" fact and captures its attributes and value.
var pctFact = regexp.MustCompile(`<(?:[\w-]+:)?ShareholdingAsAPercentageOfTotalNumberOfShares\b([^>]*)>\s*([^<\s]+)\s*<`)
var contextRef = regexp.MustCompile(`contextRef="([^"]+)"`)

// parseHolding reads the FII and DII totals from a shareholding-pattern XBRL
// filing. Values there are fractions of total shares; they are returned in %.
func parseHolding(body []byte) (holding, error) {
	h := holding{fii: math.NaN(), dii: math.NaN()}
	hasPublic := false
	for _, m := range pctFact.FindAllSubmatch(body, -1) {
		ref := contextRef.FindSubmatch(m[1])
		if ref == nil {
			continue
		}
		var target *float64
		switch string(ref[1]) {
		case "InstitutionsForeign_ContextI":
			target = &h.fii
		case "InstitutionsDomestic_ContextI":
			target = &h.dii
		case "PublicShareholding_ContextI":
			hasPublic = true
			continue
		default:
			continue
		}
		if v, err := strconv.ParseFloat(string(m[2]), 64); err == nil {
			*target = v * 100
		}
	}
	// A complete filing leaves out investor groups that hold nothing (common for
	// small companies with no institutional investors), so those are 0%.
	if hasPublic {
		if math.IsNaN(h.fii) {
			h.fii = 0
		}
		if math.IsNaN(h.dii) {
			h.dii = 0
		}
	}
	if math.IsNaN(h.fii) && math.IsNaN(h.dii) {
		return h, errors.New("no FII/DII totals in the filing")
	}
	return h, nil
}
