// Package provider fetches daily price history. The scanner depends only on
// the Provider interface, so Yahoo can be swapped for a broker API.
package provider

import (
	"context"

	"stockscanner/internal/indicators"
)

// Series is one ticker's daily bars, oldest first.
type Series struct {
	Ticker string
	Name   string
	Bars   []indicators.Bar
}

// Provider returns daily history for each ticker. A ticker that fails appears
// in errs instead of failing the whole call.
type Provider interface {
	History(ctx context.Context, tickers []string) (series map[string]Series, errs map[string]error)
}

// IntradayProvider is optionally implemented by providers that can return the
// latest session's intraday bars (Bar.Date holds each bar's start time).
type IntradayProvider interface {
	Intraday(ctx context.Context, tickers []string) (series map[string]Series, errs map[string]error)
}
