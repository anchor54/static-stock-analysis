// Command holdings refreshes the saved FII/DII holdings snapshot
// (internal/fundamentals/holdings.json) from NSE's shareholding-pattern
// filings. NSE blocks most cloud servers, so run it from your own computer,
// about once a quarter after companies file (by the 21st of January, April,
// July and October), then commit and push the file:
//
//	npm run refresh-holdings
//	go run ./cmd/holdings -symbols RELIANCE,TCS   # just these stocks
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"stockscanner/internal/fundamentals"
	"stockscanner/internal/symbols"
)

func main() {
	out := flag.String("out", fundamentals.SnapshotFile, "snapshot file to update")
	dataDir := flag.String("data", "data", "directory for the cached NSE symbol list")
	only := flag.String("symbols", "", "comma-separated NSE symbols to refresh (default: every NSE equity)")
	full := flag.Bool("full", false, "download every stock's filings, even when its latest quarter is already saved")
	workers := flag.Int("concurrency", 3, "stocks fetched at once (keep it low to stay polite to NSE)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	snap, err := fundamentals.LoadSnapshot(*out)
	if err != nil {
		log.Fatal(err)
	}

	var syms []string
	if *only != "" {
		for _, s := range strings.Split(*only, ",") {
			if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
				syms = append(syms, s)
			}
		}
	} else {
		list, err := symbols.NewSource(*dataDir).Get(ctx)
		if err != nil {
			log.Fatalf("NSE symbol list unavailable: %v", err)
		}
		for _, e := range list.Equities {
			syms = append(syms, strings.ToUpper(e.Symbol))
		}
	}
	fmt.Printf("Refreshing FII/DII holdings for %d stocks into %s (Ctrl+C stops and saves progress)\n", len(syms), *out)

	src := fundamentals.New()
	var (
		mu                          sync.Mutex
		done, updated, kept, failed atomic.Int32
		blockedRun                  atomic.Int32
		failures                    []string
		start                       = time.Now()
	)
	save := func() {
		mu.Lock()
		defer mu.Unlock()
		snap.GeneratedAt = time.Now().Format("2006-01-02")
		if err := snap.Save(*out); err != nil {
			log.Printf("saving: %v", err)
		}
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	for range max(*workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				status, err := refresh(ctx, src, snap, &mu, sym, *full)
				switch {
				case err == nil && status == "updated":
					updated.Add(1)
					blockedRun.Store(0)
				case err == nil:
					kept.Add(1)
					blockedRun.Store(0)
				default:
					failed.Add(1)
					mu.Lock()
					failures = append(failures, fmt.Sprintf("%s: %v", sym, err))
					mu.Unlock()
					if fundamentals.IsBlocked(err) {
						blockedRun.Add(1)
					}
				}
				n := done.Add(1)
				if n%100 == 0 {
					fmt.Printf("  %d/%d · %d updated, %d unchanged, %d failed · %s\n",
						n, len(syms), updated.Load(), kept.Load(), failed.Load(), time.Since(start).Round(time.Second))
					save()
				}
			}
		}()
	}

feed:
	for _, s := range syms {
		if blockedRun.Load() >= 10 {
			fmt.Println("NSE refused 10 requests in a row; stopping. Wait a while and run again: saved stocks are skipped.")
			break
		}
		select {
		case jobs <- s:
		case <-ctx.Done():
			fmt.Println("Stopped; saving progress.")
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	save()

	fmt.Printf("Done in %s: %d updated, %d unchanged, %d failed. %d stocks in the snapshot.\n",
		time.Since(start).Round(time.Second), updated.Load(), kept.Load(), failed.Load(), len(snap.Stocks))
	if n := len(failures); n > 0 {
		fmt.Printf("Failures (first %d of %d):\n", min(n, 15), n)
		for _, f := range failures[:min(n, 15)] {
			fmt.Println("  " + f)
		}
	}
	fmt.Println("Next: commit and push", *out, "to update the deployed site.")
}

// refresh updates one stock, skipping the filing downloads when the saved
// entry already has NSE's latest two quarters.
func refresh(ctx context.Context, src *fundamentals.Source, snap *fundamentals.Snapshot, mu *sync.Mutex, sym string, full bool) (string, error) {
	var filings []fundamentals.Filing
	var err error
	for attempt := 0; ; attempt++ {
		filings, err = src.Filings(ctx, sym)
		if err == nil || !fundamentals.IsBlocked(err) || attempt == 1 {
			break
		}
		// NSE throttles bursts: back off, get fresh cookies, try once more.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(15 * time.Second):
		}
		src.RefreshCookies(ctx)
	}
	if err != nil {
		return "", err
	}

	mu.Lock()
	old, have := snap.Stocks[sym]
	mu.Unlock()
	latest := fundamentals.QuarterDate(filings[0].Date)
	prev := ""
	if len(filings) > 1 {
		prev = fundamentals.QuarterDate(filings[1].Date)
	}
	if have && !full && old.Quarter == latest && old.PrevQuarter == prev {
		return "unchanged", nil
	}

	h, err := src.HoldingFrom(ctx, filings)
	if err != nil {
		return "", err
	}
	mu.Lock()
	snap.Stocks[sym] = fundamentals.EntryOf(h)
	mu.Unlock()
	return "updated", nil
}
