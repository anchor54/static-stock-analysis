# Stock Scanner

Paste a list of NSE stocks (symbols, ISINs or company names) and get one sortable table of
RS55 vs Nifty 50, RSI (14) daily/weekly/monthly, SMA 9/20/50/200, classic pivots, % from
daily VWAP and Weinstein stage. Implements *Stock Scanner — Product Design Doc* (v1) with a Go backend.

## Run

Requires Go 1.22+ and Node 20+. Install once:

```sh
npm install        # also installs the front end's packages
```

Then, from the project root:

| Command | What it does | Open |
|---|---|---|
| `npm start` | Builds the React app, then runs the Go server, which serves the page and the API | http://127.0.0.1:8000 |
| `npm run dev` | Runs the Go API and the Vite dev server together, with hot reload; Ctrl+C stops both | http://localhost:5173 (opens automatically) |
| `npm run build` | Builds a standalone `scanner` binary with the page embedded | run `scanner` / `scanner.exe` |
| `npm test` | Go tests (no network) and the TypeScript type check | |

The built binary needs neither Go nor Node; double-click `scanner.exe` and open http://127.0.0.1:8000.
Without Node, `go run ./cmd/scanner` also works, using the checked-in `web/dist`.

## Front end

React 19 + TypeScript, built with Vite, in `frontend/`. Requires Node 20+.

In `npm run dev`, Vite serves the page and forwards `/api` calls to the Go server on :8000, so
both run as one app at http://localhost:5173. `npm run build` writes `web/dist`, which the Go
binary embeds.

| File | Purpose |
|---|---|
| `src/App.tsx` | Page layout and state: input, scan, sort, toggles |
| `src/components/StockPicker.tsx` | Searchable stock dropdown over `GET /api/symbols` |
| `src/components/ResultsTable.tsx` | Grouped, sticky-header, sortable results table |
| `src/columns.tsx` | Column definitions: how each cell renders and sorts |
| `src/copyTable.ts`, `src/format.ts`, `src/api.ts`, `src/types.ts` | Copy for Excel, formatting, API client, API types |

Flags (`go run ./cmd/scanner -h`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:8000` | Listen address |
| `-data` | `data` | Directory for the cached NSE symbol list |
| `-cache-ttl` | `15m` | How long downloaded prices are reused |
| `-benchmark` | `^NSEI` | Benchmark ticker |
| `-stage-ma-weeks`, `-stage-slope-weeks`, `-stage-flat-pct`, `-stage-band-pct`, `-stage-early-weeks`, `-stage-prior-weeks` | 30, 4, 1, 5, 8, 26 | Weinstein thresholds |

## Layout

```
cmd/scanner/          entry point: flags, wiring, HTTP listen (go build ./cmd/scanner → scanner.exe)
internal/indicators/  pure indicator engine: bars, RSI, SMA, pivots, RS55, stage (no network)
internal/provider/    Provider interface (History(tickers)), Yahoo chart API client, 15-min cache
internal/symbols/     NSE equity list (disk cache, 7-day refresh) and input resolver
internal/scanner/     input parsing, orchestration, API response types
internal/api/         POST /api/analyze, static file serving
frontend/             React + TypeScript source (Vite)
web/                  embeds the built front end (web/dist) into the binary
```

## API

`POST /api/analyze` with `{"symbols": "RELIANCE, TCS\nTata Motors"}` (string or list of strings).
Returns `rows[]`, `problems[]`, `benchmark`, `notices[]` and `generated_at`; NaN values are `null`.
Errors: `400` for empty input or more than 200 entries, `502` when Nifty 50 data cannot be downloaded.

`GET /api/symbols` returns the cached NSE equity list (`symbol`, `name`, `isin`) for the page's
stock picker, or `503` when the list is unavailable.

## Differences from the design doc

- **Backend is Go, not Python/FastAPI.** Prices come straight from Yahoo's chart API
  (`/v8/finance/chart`), whose `close` is split- and bonus-adjusted but not dividend-adjusted,
  the same series yfinance's `Close` uses. Tickers are fetched in batches of 50, 8 at a time,
  with retries on 429/5xx.
- **Front end is React + TypeScript with a Vite build**, not a plain-JavaScript page without a
  build step. The output is still embedded in the single Go binary.
- **Additions:** `notices[]` (symbol list stale or unavailable), `query`, `ticker`, `date`,
  `match` (how the entry was resolved) and `candidates` on each row. A row warning appears when
  Yahoo is missing a recent session that Nifty traded, because the day change and daily pivot then
  skip that day. A second entry resolving to an already-listed stock goes to `problems[]` as a
  duplicate.
- **Daily VWAP column (not in the doc).** Each row has `vwap`, `vwap_dist` and `vwap_date`: the
  session VWAP of the latest trading session, Σ((H+L+C)/3 × volume) ÷ Σ volume over Yahoo's
  5-minute bars, and the close's % distance from it. During market hours it covers the session
  so far. This costs one extra small request per ticker, cached for 15 minutes like daily
  prices; a cold 50-stock scan measured 2.6 s. If intraday data is missing, the row still shows,
  with `vwap: null` and a warning. Price sources can provide intraday bars by implementing the
  optional `provider.IntradayProvider` interface.
- **Not supported:** numeric BSE codes such as `500325.BO`, because Yahoo serves no data for them.
  They show up as skipped. Use `RELIANCE.BO`-style tickers instead.

## Notes

- Data is Yahoo Finance, unofficial and for personal use only (see the design doc's risks).
- Weekly and monthly pivots use the previous completed week or month until the current one ends
  (the doc's open question on switching after Friday's close is not changed).
