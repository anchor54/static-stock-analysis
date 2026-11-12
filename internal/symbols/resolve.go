package symbols

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Match is a resolved input entry.
type Match struct {
	Query      string
	Ticker     string // Yahoo ticker, e.g. TCS.NS
	Symbol     string // display symbol, e.g. TCS
	Name       string // company name if known
	Method     string // ticker, symbol, isin, name, prefix, fuzzy, symbol-only
	Note       string // ambiguity or fuzzy-match note for the row
	Candidates []string
}

// Resolver turns entries into Yahoo tickers. A nil List means symbol-only mode.
type Resolver struct {
	List *List
}

var (
	tickerRe = regexp.MustCompile(`^[A-Z0-9&_\-.]+\.(NS|BO)$`)
	isinRe   = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{9}[0-9]$`)
	symbolRe = regexp.MustCompile(`^[A-Z0-9&_\-]+$`)
)

const (
	fuzzyThreshold = 0.8
	maxCandidates  = 5
)

// Resolve tries, in order: explicit Yahoo ticker, NSE symbol, ISIN, exact
// company name, name prefix, fuzzy name match.
func (r *Resolver) Resolve(query string) (Match, error) {
	q := strings.TrimSpace(query)
	up := strings.ToUpper(q)
	m := Match{Query: q}

	if strings.HasPrefix(up, "^") || tickerRe.MatchString(up) {
		m.Ticker, m.Method = up, "ticker"
		m.Symbol = strings.TrimSuffix(up, ".NS")
		if r.List != nil && strings.HasSuffix(up, ".NS") {
			if i, ok := r.List.bySymbol[m.Symbol]; ok {
				m.Name = r.List.Equities[i].Name
			}
		}
		return m, nil
	}

	if r.List == nil {
		switch {
		case isinRe.MatchString(up) && strings.ContainsAny(up, "0123456789"):
			return m, fmt.Errorf("ISIN lookup needs the NSE symbol list, which is unavailable")
		case symbolRe.MatchString(up):
			m.Ticker, m.Symbol, m.Method = up+".NS", up, "symbol-only"
			m.Note = "NSE symbol list unavailable; treated as an NSE symbol"
			return m, nil
		default:
			return m, fmt.Errorf("name lookup needs the NSE symbol list, which is unavailable")
		}
	}
	l := r.List

	if i, ok := l.bySymbol[up]; ok {
		return l.match(m, i, "symbol"), nil
	}
	if isinRe.MatchString(up) {
		if i, ok := l.byISIN[up]; ok {
			return l.match(m, i, "isin"), nil
		}
		if strings.HasPrefix(up, "IN") {
			return m, fmt.Errorf("ISIN not found in the NSE equity list")
		}
	}

	qn := normalizeName(q)
	if qn == "" {
		return m, fmt.Errorf("not a valid symbol, ISIN or name")
	}
	if idx := l.byName[qn]; len(idx) > 0 {
		m = l.match(m, idx[0], "name")
		if len(idx) > 1 {
			m.Candidates = l.symbolsOf(idx[1:])
			m.Note = "Ambiguous name; also matches " + strings.Join(m.Candidates, ", ")
		}
		return m, nil
	}

	var prefix []int
	for i, n := range l.norm {
		if strings.HasPrefix(n, qn) {
			prefix = append(prefix, i)
		}
	}
	if len(prefix) > 0 {
		sort.SliceStable(prefix, func(a, b int) bool { return len(l.norm[prefix[a]]) < len(l.norm[prefix[b]]) })
		m = l.match(m, prefix[0], "prefix")
		if len(prefix) > 1 {
			m.Candidates = l.symbolsOf(prefix[1:])
			m.Note = fmt.Sprintf("Ambiguous: %q matches %d companies; picked %s. Others: %s",
				q, len(prefix), m.Symbol, strings.Join(m.Candidates, ", "))
		}
		return m, nil
	}

	type scored struct {
		i     int
		score float64
	}
	var hits []scored
	for i, n := range l.norm {
		if s := similarity(qn, n); s >= fuzzyThreshold-0.1 {
			hits = append(hits, scored{i, s})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	if len(hits) == 0 || hits[0].score < fuzzyThreshold {
		return m, fmt.Errorf("no NSE stock matches this symbol, ISIN or name")
	}
	m = l.match(m, hits[0].i, "fuzzy")
	var others []int
	for _, h := range hits[1:] {
		others = append(others, h.i)
	}
	m.Candidates = l.symbolsOf(others)
	m.Note = fmt.Sprintf("Fuzzy match for %q → %s (%.0f%% similar); check this is the right stock", q, l.Equities[hits[0].i].Name, hits[0].score*100)
	if len(m.Candidates) > 0 {
		m.Note += ". Other candidates: " + strings.Join(m.Candidates, ", ")
	}
	return m, nil
}

func (l *List) match(m Match, i int, method string) Match {
	e := l.Equities[i]
	m.Ticker, m.Symbol, m.Name, m.Method = strings.ToUpper(e.Symbol)+".NS", strings.ToUpper(e.Symbol), e.Name, method
	return m
}

func (l *List) symbolsOf(idx []int) []string {
	var out []string
	for _, i := range idx {
		if len(out) == maxCandidates {
			break
		}
		out = append(out, l.Equities[i].Symbol)
	}
	return out
}

var nameSuffixes = []string{"limited", "ltd", "co", "company", "corp", "corporation", "inc"}

// normalizeName lowercases, maps & to "and", drops punctuation and trailing
// corporate suffixes such as "Limited".
func normalizeName(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "&", " and "))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	for len(words) > 1 && contains(nameSuffixes, words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// similarity is 1 − Levenshtein distance ÷ length of the longer string.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 && len(rb) == 0 {
		return 1
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return 1 - float64(prev[len(rb)])/float64(max(len(ra), len(rb)))
}
