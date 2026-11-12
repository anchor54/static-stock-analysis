package provider

import (
	"context"
	"sync"
	"time"
)

// Cached keeps successful series in memory for TTL, so only tickers not
// already cached are downloaded. Daily and intraday series are cached
// separately.
type Cached struct {
	Inner Provider
	TTL   time.Duration
	Now   func() time.Time // for tests; defaults to time.Now

	mu       sync.Mutex
	daily    map[string]cacheEntry
	intraday map[string]cacheEntry
}

type cacheEntry struct {
	series  Series
	expires time.Time
}

// NewCached wraps p with a TTL cache.
func NewCached(p Provider, ttl time.Duration) *Cached {
	return &Cached{Inner: p, TTL: ttl}
}

func (c *Cached) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// History serves cached daily series and downloads the rest.
func (c *Cached) History(ctx context.Context, tickers []string) (map[string]Series, map[string]error) {
	return c.get(ctx, &c.daily, tickers, c.Inner.History)
}

// Intraday serves cached intraday series and downloads the rest. If the inner
// provider has no intraday data it returns nothing, without errors.
func (c *Cached) Intraday(ctx context.Context, tickers []string) (map[string]Series, map[string]error) {
	ip, ok := c.Inner.(IntradayProvider)
	if !ok {
		return map[string]Series{}, map[string]error{}
	}
	return c.get(ctx, &c.intraday, tickers, ip.Intraday)
}

func (c *Cached) get(ctx context.Context, entries *map[string]cacheEntry, tickers []string,
	fetch func(context.Context, []string) (map[string]Series, map[string]error)) (map[string]Series, map[string]error) {
	out := make(map[string]Series, len(tickers))
	var missing []string

	now := c.now()
	c.mu.Lock()
	for _, t := range tickers {
		if e, ok := (*entries)[t]; ok && now.Before(e.expires) {
			out[t] = e.series
		} else {
			missing = append(missing, t)
		}
	}
	c.mu.Unlock()

	if len(missing) == 0 {
		return out, map[string]error{}
	}
	fetched, errs := fetch(ctx, missing)

	c.mu.Lock()
	defer c.mu.Unlock()
	if *entries == nil {
		*entries = make(map[string]cacheEntry)
	}
	expires := c.now().Add(c.TTL)
	for t, s := range fetched {
		(*entries)[t] = cacheEntry{series: s, expires: expires}
		out[t] = s
	}
	for t, e := range *entries {
		if !now.Before(e.expires) {
			delete(*entries, t)
		}
	}
	return out, errs
}
