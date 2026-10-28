package addressing

import "strings"

// AddressTermSources is the ordered fallback chain for 称谓词/集体称呼:
// room configuration first, then the anchor style, then the script preset.
// The first non-empty source wins; later sources never override it.
type AddressTermSources struct {
	Room   []string
	Style  []string
	Script []string
}

// Resolve returns the winning terms and where they came from.
func (s AddressTermSources) Resolve() ([]string, string) {
	for _, source := range []struct {
		name  string
		terms []string
	}{
		{"room", s.Room},
		{"style", s.Style},
		{"script", s.Script},
	} {
		if terms := cleanTerms(source.terms); len(terms) > 0 {
			return terms, source.name
		}
	}
	return nil, ""
}

// ResolveGroupLabels applies the same chain to 集体称呼.
func (s AddressTermSources) ResolveGroupLabels() ([]string, string) {
	return s.Resolve()
}

func cleanTerms(terms []string) []string {
	out := make([]string, 0, len(terms))
	seen := map[string]bool{}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}
