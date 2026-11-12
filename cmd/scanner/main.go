// Command scanner runs the Stock Scanner web app locally.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"stockscanner/internal/api"
	"stockscanner/internal/fundamentals"
	"stockscanner/internal/provider"
	"stockscanner/internal/scanner"
	"stockscanner/internal/symbols"
	"stockscanner/web"
)

func main() {
	cfg := scanner.DefaultConfig()
	defaultAddr := "127.0.0.1:8000"
	if port := os.Getenv("PORT"); port != "" {
		// Hosted (e.g. Render): listen on all interfaces on the given port.
		defaultAddr = "0.0.0.0:" + port
	}
	addr := flag.String("addr", defaultAddr, "listen address (defaults to 0.0.0.0:$PORT when PORT is set)")
	dataDir := flag.String("data", "data", "directory for the cached NSE symbol list")
	cacheTTL := flag.Duration("cache-ttl", 15*time.Minute, "how long downloaded prices are reused")
	flag.StringVar(&cfg.Benchmark, "benchmark", cfg.Benchmark, "Yahoo ticker of the benchmark index")
	flag.IntVar(&cfg.Stage.MAWeeks, "stage-ma-weeks", cfg.Stage.MAWeeks, "Weinstein: weekly SMA length")
	flag.IntVar(&cfg.Stage.SlopeWeeks, "stage-slope-weeks", cfg.Stage.SlopeWeeks, "Weinstein: weeks over which the MA slope is measured")
	flag.Float64Var(&cfg.Stage.FlatPct, "stage-flat-pct", cfg.Stage.FlatPct, "Weinstein: slope within ±this % counts as flat")
	flag.Float64Var(&cfg.Stage.BandPct, "stage-band-pct", cfg.Stage.BandPct, "Weinstein: pullback/bounce band around the MA, in %")
	flag.IntVar(&cfg.Stage.EarlyWeeks, "stage-early-weeks", cfg.Stage.EarlyWeeks, "Weinstein: look-back for the \"early\" qualifier")
	flag.IntVar(&cfg.Stage.PriorWeeks, "stage-prior-weeks", cfg.Stage.PriorWeeks, "Weinstein: look-back for a flat MA's prior trend")
	flag.Parse()

	if cfg.Stage.MAWeeks < 1 || cfg.Stage.SlopeWeeks < 1 {
		log.Fatal("stage-ma-weeks and stage-slope-weeks must be at least 1")
	}

	fund := fundamentals.New()
	// Used when NSE blocks live requests, as it does on most cloud hosts.
	fund.Snapshot = fundamentals.Embedded()

	srv := &api.Server{
		Scanner: &scanner.Scanner{
			Provider:     provider.NewCached(provider.NewYahoo(), *cacheTTL),
			Symbols:      symbols.NewSource(*dataDir),
			Fundamentals: fund,
			Config:       cfg,
		},
		Static: web.Static(),
	}
	fmt.Printf("Stock Scanner running at http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
