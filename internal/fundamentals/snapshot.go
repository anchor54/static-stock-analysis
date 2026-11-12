package fundamentals

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// SnapshotFile is where `npm run refresh-holdings` saves holdings, relative to
// the project root. It is embedded into the server binary.
const SnapshotFile = "internal/fundamentals/holdings.json"

//go:embed holdings.json
var embeddedSnapshot []byte

// Snapshot is a saved copy of FII/DII holdings by NSE symbol, used when NSE
// can't be reached live.
type Snapshot struct {
	GeneratedAt string               `json:"generated_at"` // YYYY-MM-DD
	Stocks      map[string]SnapEntry `json:"stocks"`
}

// SnapEntry is one stock in the snapshot file. Missing values are omitted
// (JSON has no NaN).
type SnapEntry struct {
	Quarter     string   `json:"q"`
	FII         *float64 `json:"fii,omitempty"`
	DII         *float64 `json:"dii,omitempty"`
	PrevQuarter string   `json:"pq,omitempty"`
	FIIPrev     *float64 `json:"pfii,omitempty"`
	DIIPrev     *float64 `json:"pdii,omitempty"`
}

// Embedded returns the snapshot built into the binary, or nil if it is empty
// or unreadable.
func Embedded() *Snapshot {
	s, err := ParseSnapshot(embeddedSnapshot)
	if err != nil || len(s.Stocks) == 0 {
		return nil
	}
	return s
}

// ParseSnapshot reads a snapshot file's contents.
func ParseSnapshot(b []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("reading holdings snapshot: %w", err)
	}
	if s.Stocks == nil {
		s.Stocks = map[string]SnapEntry{}
	}
	return &s, nil
}

// LoadSnapshot reads a snapshot file; a missing file gives an empty snapshot.
func LoadSnapshot(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Snapshot{Stocks: map[string]SnapEntry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSnapshot(b)
}

// Save writes the snapshot with one stock per line, sorted, so refreshes give
// small, readable git diffs.
func (s *Snapshot) Save(path string) error {
	syms := make([]string, 0, len(s.Stocks))
	for k := range s.Stocks {
		syms = append(syms, k)
	}
	sort.Strings(syms)
	var b strings.Builder
	fmt.Fprintf(&b, "{\"generated_at\": %q,\n\"stocks\": {", s.GeneratedAt)
	for i, sym := range syms {
		e, err := marshal(s.Stocks[sym])
		if err != nil {
			return err
		}
		k, _ := marshal(sym)
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n%s: %s", k, e)
	}
	b.WriteString("\n}}\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// marshal encodes v without escaping &, < and > (symbols like M&M stay readable).
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// Date is the day the snapshot was made.
func (s *Snapshot) Date() string {
	if s == nil {
		return ""
	}
	return s.GeneratedAt
}

// Lookup returns a stock's saved holdings. It is safe on a nil snapshot.
func (s *Snapshot) Lookup(symbol string) (Holding, bool) {
	if s == nil {
		return Holding{}, false
	}
	e, ok := s.Stocks[strings.ToUpper(symbol)]
	if !ok {
		return Holding{}, false
	}
	return e.Holding(), true
}

// Holding converts the entry, with NaN for missing values.
func (e SnapEntry) Holding() Holding {
	f := func(p *float64) float64 {
		if p == nil {
			return math.NaN()
		}
		return *p
	}
	return Holding{
		Quarter: e.Quarter, PrevQuarter: e.PrevQuarter,
		FII: f(e.FII), DII: f(e.DII), FIIPrev: f(e.FIIPrev), DIIPrev: f(e.DIIPrev),
	}
}

// EntryOf converts a holding for saving, rounding to 2 decimals as filed.
func EntryOf(h Holding) SnapEntry {
	p := func(v float64) *float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		r := math.Round(v*100) / 100
		return &r
	}
	return SnapEntry{
		Quarter: h.Quarter, PrevQuarter: h.PrevQuarter,
		FII: p(h.FII), DII: p(h.DII), FIIPrev: p(h.FIIPrev), DIIPrev: p(h.DIIPrev),
	}
}
