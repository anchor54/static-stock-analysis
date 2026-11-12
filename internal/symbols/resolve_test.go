package symbols

import (
	"strings"
	"testing"
)

const sampleCSV = "SYMBOL,NAME OF COMPANY, SERIES, DATE OF LISTING, PAID UP VALUE, MARKET LOT, ISIN NUMBER, FACE VALUE\n" +
	"TCS,Tata Consultancy Services Limited,EQ,25-AUG-2004,1,1,INE467B01029,1\n" +
	"TATAMOTORS,Tata Motors Limited,EQ,22-JUL-1998,2,1,INE155A01022,2\n" +
	"TATASTEEL,Tata Steel Limited,EQ,17-NOV-1998,1,1,INE081A01020,1\n" +
	"RELIANCE,Reliance Industries Limited,EQ,29-NOV-1995,10,1,INE002A01018,10\n" +
	"M&M,Mahindra & Mahindra Limited,EQ,17-NOV-1995,5,1,INE101A01026,5\n"

func sampleList(t *testing.T) *List {
	t.Helper()
	eq, err := ParseCSV(strings.NewReader(sampleCSV))
	if err != nil {
		t.Fatal(err)
	}
	return NewList(eq)
}

func TestResolve(t *testing.T) {
	r := Resolver{List: sampleList(t)}
	cases := []struct {
		in, ticker, method string
		note               bool
	}{
		{"tcs.ns", "TCS.NS", "ticker", false},
		{"500325.BO", "500325.BO", "ticker", false},
		{"^NSEI", "^NSEI", "ticker", false},
		{"reliance", "RELIANCE.NS", "symbol", false},
		{"m&m", "M&M.NS", "symbol", false},
		{"INE155A01022", "TATAMOTORS.NS", "isin", false},
		{"Tata Motors", "TATAMOTORS.NS", "name", false},
		{"Mahindra and Mahindra Ltd.", "M&M.NS", "name", false},
		{"Tata", "TATASTEEL.NS", "prefix", true},
		{"Reliance Industris", "RELIANCE.NS", "fuzzy", true},
	}
	for _, c := range cases {
		m, err := r.Resolve(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if m.Ticker != c.ticker || m.Method != c.method || (m.Note != "") != c.note {
			t.Errorf("%q: got %s via %s (note %q), want %s via %s", c.in, m.Ticker, m.Method, m.Note, c.ticker, c.method)
		}
	}

	for _, bad := range []string{"Infosys", "INE999Z99999", "zz"} {
		if _, err := r.Resolve(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestResolveSymbolOnly(t *testing.T) {
	r := Resolver{}
	if m, err := r.Resolve("infy"); err != nil || m.Ticker != "INFY.NS" || m.Note == "" {
		t.Errorf("symbol-only: %+v, %v", m, err)
	}
	if _, err := r.Resolve("Tata Motors"); err == nil {
		t.Error("names need the symbol list")
	}
	if _, err := r.Resolve("INE155A01022"); err == nil {
		t.Error("ISINs need the symbol list")
	}
}
