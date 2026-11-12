package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"stockscanner/internal/indicators"
)

// Yahoo reads daily bars from Yahoo Finance's chart API. Its Close is split-
// and bonus-adjusted but not dividend-adjusted, matching TradingView's default.
type Yahoo struct {
	Client      *http.Client
	BaseURL     string // default https://query1.finance.yahoo.com
	Range       string // default 5y
	BatchSize   int    // tickers per batch, default 50
	Concurrency int    // parallel requests within a batch, default 8
}

// NewYahoo returns a Yahoo provider with the design-doc defaults.
func NewYahoo() *Yahoo {
	return &Yahoo{
		Client:      &http.Client{Timeout: 20 * time.Second},
		BaseURL:     "https://query1.finance.yahoo.com",
		Range:       "5y",
		BatchSize:   50,
		Concurrency: 8,
	}
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36"

// History downloads Range of daily bars for each ticker.
func (y *Yahoo) History(ctx context.Context, tickers []string) (map[string]Series, map[string]error) {
	return y.each(ctx, tickers, func(t string) (Series, error) { return y.fetchChart(ctx, t, y.Range, "1d", true) })
}

// Intraday downloads the latest session's 5-minute bars for each ticker.
func (y *Yahoo) Intraday(ctx context.Context, tickers []string) (map[string]Series, map[string]error) {
	return y.each(ctx, tickers, func(t string) (Series, error) { return y.fetchChart(ctx, t, "1d", "5m", false) })
}

// each runs fetch for every ticker in batches of BatchSize, Concurrency at a time.
func (y *Yahoo) each(ctx context.Context, tickers []string, fetch func(string) (Series, error)) (map[string]Series, map[string]error) {
	series := make(map[string]Series, len(tickers))
	errs := make(map[string]error)
	var mu sync.Mutex

	batch := max(y.BatchSize, 1)
	for start := 0; start < len(tickers); start += batch {
		sem := make(chan struct{}, max(y.Concurrency, 1))
		var wg sync.WaitGroup
		for _, t := range tickers[start:min(start+batch, len(tickers))] {
			wg.Add(1)
			sem <- struct{}{}
			go func(t string) {
				defer wg.Done()
				defer func() { <-sem }()
				s, err := fetch(t)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs[t] = err
				} else {
					series[t] = s
				}
			}(t)
		}
		wg.Wait()
	}
	return series, errs
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol    string `json:"symbol"`
				LongName  string `json:"longName"`
				ShortName string `json:"shortName"`
				GMTOffset int    `json:"gmtoffset"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// fetchChart downloads one ticker's bars. With daily set, each bar's Date is
// its exchange-local calendar date and repeated dates are merged; otherwise
// Date is the bar's exchange-local start time. Either way it is stored in UTC.
func (y *Yahoo) fetchChart(ctx context.Context, ticker, rng, interval string, daily bool) (Series, error) {
	u := fmt.Sprintf("%s/v8/finance/chart/%s?range=%s&interval=%s&includePrePost=false&events=div%%7Csplit",
		y.BaseURL, url.PathEscape(ticker), rng, interval)

	var body []byte
	var status int
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return Series{}, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := y.Client.Do(req)
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			resp.Body.Close()
			status = resp.StatusCode
		}
		retryable := err != nil || status == http.StatusTooManyRequests || status >= 500
		if !retryable || attempt == 2 {
			if err != nil {
				return Series{}, fmt.Errorf("download failed: %w", err)
			}
			break
		}
		select {
		case <-ctx.Done():
			return Series{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}

	var cr chartResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return Series{}, fmt.Errorf("Yahoo returned HTTP %d with an unreadable body", status)
	}
	if cr.Chart.Error != nil {
		return Series{}, fmt.Errorf("Yahoo: %s", cr.Chart.Error.Description)
	}
	if status != http.StatusOK || len(cr.Chart.Result) == 0 {
		return Series{}, fmt.Errorf("Yahoo returned HTTP %d", status)
	}
	res := cr.Chart.Result[0]
	if len(res.Indicators.Quote) == 0 {
		return Series{}, errors.New("no price data")
	}
	q := res.Indicators.Quote[0]
	loc := time.FixedZone("exchange", res.Meta.GMTOffset)

	at := func(xs []*float64, i int) (float64, bool) {
		if i < len(xs) && xs[i] != nil {
			return *xs[i], true
		}
		return 0, false
	}
	var bars []indicators.Bar
	for i, ts := range res.Timestamp {
		c, ok := at(q.Close, i)
		if !ok {
			continue // holiday or missing row
		}
		local := time.Unix(ts, 0).In(loc)
		yr, mo, dy := local.Date()
		date := time.Date(yr, mo, dy, 0, 0, 0, 0, time.UTC)
		if !daily {
			date = time.Date(yr, mo, dy, local.Hour(), local.Minute(), 0, 0, time.UTC)
		}
		b := indicators.Bar{Date: date, Open: c, High: c, Low: c, Close: c}
		if v, ok := at(q.Open, i); ok {
			b.Open = v
		}
		if v, ok := at(q.High, i); ok {
			b.High = v
		}
		if v, ok := at(q.Low, i); ok {
			b.Low = v
		}
		b.Volume, _ = at(q.Volume, i)
		// Yahoo sometimes repeats the live session; keep the latest row per date.
		if n := len(bars); daily && n > 0 && bars[n-1].Date.Equal(b.Date) {
			bars[n-1] = b
			continue
		}
		bars = append(bars, b)
	}
	if len(bars) == 0 {
		return Series{}, errors.New("no price data")
	}
	name := res.Meta.LongName
	if name == "" {
		name = res.Meta.ShortName
	}
	return Series{Ticker: ticker, Name: name, Bars: bars}, nil
}
