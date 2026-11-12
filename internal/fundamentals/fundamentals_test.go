package fundamentals

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const timeseriesJSON = `{"timeseries":{"result":[
 {"meta":{"symbol":["X.NS"],"type":["annualFreeCashFlow"]},"timestamp":[1,2,3],
  "annualFreeCashFlow":[
   {"asOfDate":"2024-03-31","reportedValue":{"raw":5.905E10}},
   null,
   {"asOfDate":"2026-03-31","reportedValue":{"raw":6.9197E11}},
   {"asOfDate":"2025-03-31","reportedValue":{"raw":3.8736E11}}]},
 {"meta":{"symbol":["X.NS"],"type":["annualChangesInCash"]},"timestamp":[1],
  "annualChangesInCash":[{"asOfDate":"2026-03-31","reportedValue":{"raw":-3.9475E11}}]}
],"error":null}}`

func TestParseTimeseries(t *testing.T) {
	got, err := parseTimeseries([]byte(timeseriesJSON))
	if err != nil {
		t.Fatal(err)
	}
	last, prev, date := lastTwo(got["annualFreeCashFlow"])
	if date != "2026-03-31" || math.Abs(last-69197) > 1e-6 || math.Abs(prev-38736) > 1e-6 {
		t.Errorf("FCF = %v, %v, %q; want 69197, 38736, 2026-03-31", last, prev, date)
	}
	last, prev, _ = lastTwo(got["annualChangesInCash"])
	if math.Abs(last+39475) > 1e-6 || !math.IsNaN(prev) {
		t.Errorf("net cash = %v, %v; want -39475, NaN", last, prev)
	}
}

func xbrl(fii, dii string) string {
	return fmt.Sprintf(`<xbrli:xbrl>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="MutualFundsOrUTI_ContextI" decimals="INF" unitRef="pure">0.1031</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="InstitutionsDomestic_ContextI" decimals="INF" unitRef="pure">%s</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares decimals="INF" contextRef="InstitutionsForeign_ContextI" unitRef="pure"> %s </in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
</xbrli:xbrl>`, dii, fii)
}

func TestParseHolding(t *testing.T) {
	h, err := parseHolding([]byte(xbrl("0.1667", "0.2172")))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(h.fii-16.67) > 1e-9 || math.Abs(h.dii-21.72) > 1e-9 {
		t.Errorf("got FII %v DII %v; want 16.67, 21.72", h.fii, h.dii)
	}
	// A small company with no institutional investors files only the public total.
	none := `<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="PublicShareholding_ContextI" decimals="INF" unitRef="pure">0.2689</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>`
	if h, err := parseHolding([]byte(none)); err != nil || h.fii != 0 || h.dii != 0 {
		t.Errorf("no institutions: got %+v, %v; want 0, 0", h, err)
	}
	if _, err := parseHolding([]byte("<x/>")); err == nil {
		t.Error("want an error for a filing without totals")
	}
}

func TestLatestQuartersPrefersRevision(t *testing.T) {
	got := latestQuarters([]Filing{
		{Date: "30-JUN-2026", Broadcast: "16-JUL-2026 07:24:34", XBRL: "http://x/jun"},
		{Date: "30-SEP-2026", Broadcast: "21-OCT-2026 16:25:15", XBRL: "http://x/sep"},
		{Date: "30-SEP-2026", Broadcast: "25-OCT-2026 10:00:00", XBRL: "http://x/sep-revised"},
		{Date: "31-MAR-2026", Broadcast: "21-APR-2026 01:25:04", XBRL: "http://x/mar"},
		{Date: "31-DEC-2026", Broadcast: "", XBRL: "-"},
	}, 2)
	if len(got) != 2 || got[0].XBRL != "http://x/sep-revised" || got[1].XBRL != "http://x/jun" {
		t.Errorf("got %+v", got)
	}
}

func TestGetEndToEnd(t *testing.T) {
	var nseCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/fundamentals-timeseries/v1/finance/timeseries/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, timeseriesJSON)
	})
	var srv *httptest.Server
	mux.HandleFunc("/api/corporate-share-holdings-master", func(w http.ResponseWriter, r *http.Request) {
		nseCalls.Add(1)
		if r.URL.Query().Get("symbol") != "M&M" {
			http.Error(w, "unknown", http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `[{"date":"30-SEP-2026","broadcastDate":"21-OCT-2026 16:25:15","xbrl":"%[1]s/sep.xml"},
			{"date":"30-JUN-2026","broadcastDate":"16-JUL-2026 07:24:34","xbrl":"%[1]s/jun.xml"}]`, srv.URL)
	})
	mux.HandleFunc("/sep.xml", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, xbrl("0.1667", "0.2172")) })
	mux.HandleFunc("/jun.xml", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, xbrl("0.172", "0.2119")) })
	srv = httptest.NewServer(mux)
	defer srv.Close()

	s := New()
	s.YahooURL, s.NSEURL = srv.URL, srv.URL
	got := s.Get(context.Background(), []string{"M&M.NS", "500325.BO"})

	d := got["M&M.NS"]
	if len(d.Notes) != 0 {
		t.Fatalf("notes: %v", d.Notes)
	}
	if d.Quarter != "2026-09-30" || d.PrevQuarter != "2026-06-30" ||
		math.Abs(d.FII-16.67) > 1e-9 || math.Abs(d.FIIPrev-17.2) > 1e-9 || math.Abs(d.DIIPrev-21.19) > 1e-9 {
		t.Errorf("holdings: %+v", d)
	}
	if math.Abs(d.FCF-69197) > 1e-6 || d.FCFYear != "2026-03-31" {
		t.Errorf("FCF: %v %q", d.FCF, d.FCFYear)
	}

	bse := got["500325.BO"]
	if !math.IsNaN(bse.FII) || len(bse.Notes) != 1 || !strings.Contains(bse.Notes[0], "only available for NSE") {
		t.Errorf("BSE ticker: %+v", bse)
	}

	// A second call is served from the cache.
	before := nseCalls.Load()
	s.Get(context.Background(), []string{"M&M.NS"})
	if nseCalls.Load() != before {
		t.Error("second Get hit NSE again; want the cached result")
	}
}

func TestSnapshotFallbackWhenNSEBlocks(t *testing.T) {
	var nseCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/fundamentals-timeseries/v1/finance/timeseries/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, timeseriesJSON)
	})
	mux.HandleFunc("/api/corporate-share-holdings-master", func(w http.ResponseWriter, r *http.Request) {
		nseCalls.Add(1)
		http.Error(w, "Access Denied", http.StatusForbidden)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Access Denied", http.StatusForbidden) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	snap, err := ParseSnapshot([]byte(`{"generated_at":"2026-11-12","stocks":{
		"RELIANCE":{"q":"2026-09-30","fii":16.67,"dii":21.72,"pq":"2026-06-30","pfii":17.2,"pdii":21.19}}}`))
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	s.YahooURL, s.NSEURL, s.Snapshot, s.Concurrency = srv.URL, srv.URL, snap, 1
	got := s.Get(context.Background(), []string{"RELIANCE.NS", "TCS.NS"})

	r := got["RELIANCE.NS"]
	if len(r.Notes) != 0 || r.HoldingSource != "snapshot of 2026-11-12" || math.Abs(r.FII-r.FIIPrev+0.53) > 1e-9 {
		t.Errorf("RELIANCE: %+v", r)
	}
	tcs := got["TCS.NS"]
	if !math.IsNaN(tcs.FII) || len(tcs.Notes) != 1 || !strings.Contains(tcs.Notes[0], "refresh-holdings") {
		t.Errorf("TCS: %+v", tcs)
	}
	// After the first refusal NSE is skipped, so only the first stock asked it (plus one cookie retry).
	if n := nseCalls.Load(); n > 2 {
		t.Errorf("NSE asked %d times; want at most 2", n)
	}
}

func TestCashFlowInDollarsIsConverted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/fundamentals-timeseries/v1/finance/timeseries/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"timeseries":{"result":[{"meta":{"type":["annualFreeCashFlow"]},
			"annualFreeCashFlow":[{"asOfDate":"2026-03-31","currencyCode":"USD","reportedValue":{"raw":3.733E9}}]}]}}`)
	})
	mux.HandleFunc("/v8/finance/chart/USDINR=X", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"chart":{"result":[{"meta":{"regularMarketPrice":85}}]}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New()
	s.YahooURL = srv.URL
	d := empty()
	if err := s.cashFlow(context.Background(), "INFY.NS", &d); err != nil {
		t.Fatal(err)
	}
	if math.Abs(d.FCF-373.3*85) > 1e-6 || !strings.Contains(d.CashNote, "USD") {
		t.Errorf("FCF %v, note %q; want %v ₹ Cr and a USD note", d.FCF, d.CashNote, 373.3*85)
	}
}

func TestSnapshotSaveRoundTrip(t *testing.T) {
	path := t.TempDir() + "/h.json"
	s := &Snapshot{GeneratedAt: "2026-11-12", Stocks: map[string]SnapEntry{
		"M&M":      EntryOf(Holding{Quarter: "2026-09-30", FII: 33.354, DII: 34.24, FIIPrev: math.NaN(), DIIPrev: 32.65}),
		"RELIANCE": EntryOf(Holding{Quarter: "2026-09-30", FII: 16.67, DII: 21.72}),
	}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := got.Lookup("m&m")
	if !ok || h.FII != 33.35 || !math.IsNaN(h.FIIPrev) || h.DIIPrev != 32.65 || got.Date() != "2026-11-12" {
		t.Errorf("round trip: %+v ok=%v", h, ok)
	}
}
