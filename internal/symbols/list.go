// Package symbols loads the NSE equity list and resolves user input (Yahoo
// tickers, NSE symbols, ISINs and company names) to Yahoo tickers.
package symbols

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Equity is one row of the NSE equity list.
type Equity struct {
	Symbol string
	Name   string
	ISIN   string
}

// List is the NSE equity list with lookup indexes.
type List struct {
	Equities  []Equity
	FetchedAt time.Time
	Stale     bool // the refresh failed and an old copy is in use

	bySymbol map[string]int
	byISIN   map[string]int
	byName   map[string][]int
	norm     []string // normalized names, parallel to Equities
}

// NewList builds the lookup indexes for equities.
func NewList(equities []Equity) *List {
	l := &List{
		Equities: equities,
		bySymbol: make(map[string]int, len(equities)),
		byISIN:   make(map[string]int, len(equities)),
		byName:   make(map[string][]int, len(equities)),
		norm:     make([]string, len(equities)),
	}
	for i, e := range equities {
		l.bySymbol[strings.ToUpper(e.Symbol)] = i
		if e.ISIN != "" {
			l.byISIN[strings.ToUpper(e.ISIN)] = i
		}
		n := normalizeName(e.Name)
		l.norm[i] = n
		l.byName[n] = append(l.byName[n], i)
	}
	return l
}

// ParseCSV reads NSE's EQUITY_L.csv format (SYMBOL, NAME OF COMPANY, …, ISIN NUMBER, …).
func ParseCSV(r io.Reader) ([]Equity, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(h, "\xef\xbb\xbf")))] = i
	}
	sym, okS := col["SYMBOL"]
	name, okN := col["NAME OF COMPANY"]
	isin, okI := col["ISIN NUMBER"]
	if !okS || !okN {
		return nil, errors.New("unexpected NSE equity list format")
	}
	var out []Equity
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if sym >= len(rec) || name >= len(rec) {
			continue
		}
		e := Equity{Symbol: strings.TrimSpace(rec[sym]), Name: strings.TrimSpace(rec[name])}
		if okI && isin < len(rec) {
			e.ISIN = strings.TrimSpace(rec[isin])
		}
		if e.Symbol != "" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("NSE equity list is empty")
	}
	return out, nil
}

// Source provides the NSE equity list, cached on disk and refreshed every
// MaxAge. A failed refresh falls back to the stale copy; with no copy at all
// Get returns an error and callers use symbol-only mode.
type Source struct {
	Path       string        // disk cache file
	URLs       []string      // tried in order
	MaxAge     time.Duration // 7 days
	RetryAfter time.Duration // wait before retrying a failed refresh
	Client     *http.Client

	mu          sync.Mutex
	list        *List
	lastAttempt time.Time
}

// NewSource returns a Source caching the list in dataDir.
func NewSource(dataDir string) *Source {
	return &Source{
		Path: filepath.Join(dataDir, "nse_equity_list.csv"),
		URLs: []string{
			"https://nsearchives.nseindia.com/content/equities/EQUITY_L.csv",
			"https://archives.nseindia.com/content/equities/EQUITY_L.csv",
		},
		MaxAge:     7 * 24 * time.Hour,
		RetryAfter: time.Hour,
		Client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Get returns the current list, refreshing it if it is older than MaxAge.
func (s *Source) Get(ctx context.Context) (*List, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()

	if s.list == nil {
		if l, err := s.readDisk(); err == nil {
			s.list = l
		}
	}
	if s.list != nil && now.Sub(s.list.FetchedAt) < s.MaxAge {
		return s.list, nil
	}
	if !s.lastAttempt.IsZero() && now.Sub(s.lastAttempt) < s.RetryAfter {
		if s.list != nil {
			return s.list, nil
		}
		return nil, errors.New("NSE symbol list unavailable")
	}

	s.lastAttempt = now
	l, err := s.download(ctx)
	if err == nil {
		s.list = l
		return l, nil
	}
	if s.list != nil {
		s.list.Stale = true
		return s.list, nil
	}
	return nil, fmt.Errorf("NSE symbol list unavailable: %w", err)
}

func (s *Source) readDisk() (*List, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	eq, err := ParseCSV(f)
	if err != nil {
		return nil, err
	}
	l := NewList(eq)
	l.FetchedAt = info.ModTime()
	return l, nil
}

func (s *Source) download(ctx context.Context) (*List, error) {
	var lastErr error
	for _, u := range s.URLs {
		body, err := s.get(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		eq, err := ParseCSV(strings.NewReader(string(body)))
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err == nil {
			tmp := s.Path + ".tmp"
			if os.WriteFile(tmp, body, 0o644) == nil {
				_ = os.Rename(tmp, s.Path)
			}
		}
		l := NewList(eq)
		l.FetchedAt = time.Now()
		return l, nil
	}
	return nil, lastErr
}

func (s *Source) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36")
	req.Header.Set("Accept", "text/csv,*/*")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
