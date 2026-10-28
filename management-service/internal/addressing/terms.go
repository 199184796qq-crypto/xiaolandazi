package addressing

import "strings"

// DefaultTerms are used when neither the room nor the anchor style configures
// any address term; the program owns this fallback.
var DefaultTerms = []string{"老乡", "朋友", "家人", "哥姐"}

// DefaultGroupLabels are the fallback collective addresses.
var DefaultGroupLabels = []string{"几位朋友", "几位老乡", "刚来的几位"}

// AddressTermSources is the ordered fallback chain for 称谓词/集体称呼:
// room configuration first, then the anchor style, then program defaults.
type AddressTermSources struct {
	Room  []string
	Style []string
}

// Resolve returns the winning terms and where they came from.
func (s AddressTermSources) Resolve() ([]string, string) {
	for _, source := range []struct {
		name  string
		terms []string
	}{
		{"room", s.Room},
		{"style", s.Style},
	} {
		if terms := cleanTerms(source.terms); len(terms) > 0 {
			return terms, source.name
		}
	}
	return append([]string(nil), DefaultTerms...), "default"
}

// ResolveGroupLabels applies the same chain to 集体称呼.
func (s AddressTermSources) ResolveGroupLabels() ([]string, string) {
	for _, source := range []struct {
		name  string
		terms []string
	}{
		{"room", s.Room},
		{"style", s.Style},
	} {
		if terms := cleanTerms(source.terms); len(terms) > 0 {
			return terms, source.name
		}
	}
	return append([]string(nil), DefaultGroupLabels...), "default"
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

// GroupLabelsFrom derives collective addresses from the winning address terms,
// so a room configured with 老乡/家人们 never hears the default 几位朋友.
func GroupLabelsFrom(terms []string) []string {
	labels := make([]string, 0, len(terms)*2)
	seen := map[string]bool{}
	add := func(label string) {
		label = strings.TrimSpace(label)
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		labels = append(labels, label)
	}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if strings.HasSuffix(term, "们") {
			add(term)
			continue
		}
		add(term + "们")
		add(term)
	}
	if len(labels) == 0 {
		return append([]string(nil), DefaultGroupLabels...)
	}
	return labels
}
