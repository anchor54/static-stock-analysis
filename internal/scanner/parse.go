package scanner

import "strings"

// MaxEntries is the most entries one request may contain.
const MaxEntries = 200

// ParseEntries splits input on commas, semicolons and new lines (spaces stay
// inside names), trims each entry and drops case-insensitive duplicates.
func ParseEntries(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	seen := make(map[string]bool, len(parts))
	var out []string
	for _, p := range parts {
		p = strings.Join(strings.Fields(p), " ")
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}
