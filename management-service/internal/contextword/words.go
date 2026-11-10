// Package contextword holds plan-owned language hints, never business facts.
package contextword

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Entry struct {
	ID       string   `json:"id,omitempty"`
	Triggers []string `json:"triggers"`
	Meaning  string   `json:"meaning"`
	When     string   `json:"when,omitempty"`
	Example  string   `json:"example,omitempty"`
	Enabled  bool     `json:"enabled"`
}
type Catalog struct {
	Revision int64   `json:"revision"`
	Items    []Entry `json:"items"`
}
type Hit struct {
	Matched []string `json:"matched"`
	Meaning string   `json:"meaning"`
	When    string   `json:"when,omitempty"`
	Example string   `json:"example,omitempty"`
}
type Context struct {
	Hits    []Hit `json:"hits"`
	Omitted int   `json:"omitted,omitempty"`
}

func Normalize(items []Entry) ([]Entry, error) {
	if len(items) > 1000 {
		return nil, errors.New("每个方案最多1000条语境词")
	}
	out := make([]Entry, 0, len(items))
	ids := map[string]bool{}
	for _, e := range items {
		e.Meaning = strings.TrimSpace(e.Meaning)
		e.When = strings.TrimSpace(e.When)
		e.Example = strings.TrimSpace(e.Example)
		if e.Meaning == "" || utf8.RuneCountInString(e.Meaning) > 400 || utf8.RuneCountInString(e.When) > 300 || utf8.RuneCountInString(e.Example) > 200 {
			return nil, errors.New("含义必填且最多400字，适用条件最多300字，例句最多200字")
		}
		if e.ID != "" && (len(e.ID) > 80 || ids[e.ID]) {
			return nil, errors.New("语境词身份重复或无效")
		}
		ids[e.ID] = e.ID != ""
		seen := map[string]bool{}
		triggers := []string{}
		for _, t := range e.Triggers {
			t = strings.TrimSpace(t)
			key := strings.ToLower(t)
			if t == "" {
				continue
			}
			if utf8.RuneCountInString(t) > 80 {
				return nil, errors.New("触发词最多80字")
			}
			if !seen[key] {
				seen[key] = true
				triggers = append(triggers, t)
			}
		}
		if len(triggers) == 0 || len(triggers) > 20 {
			return nil, errors.New("每条填写1至20个触发词")
		}
		e.Triggers = triggers
		for _, before := range out {
			if equalEntry(before, e) {
				return nil, errors.New("有完全重复的语境词，请保留一条")
			}
		}
		out = append(out, e)
	}
	return out, nil
}
func equalEntry(a, b Entry) bool {
	if a.Meaning != b.Meaning || a.When != b.When || a.Example != b.Example || len(a.Triggers) != len(b.Triggers) {
		return false
	}
	x := append([]string(nil), a.Triggers...)
	y := append([]string(nil), b.Triggers...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}
func contains(text, term string) bool {
	text = strings.ToLower(text)
	term = strings.ToLower(term)
	ascii := func(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') }
	first, _ := utf8.DecodeRuneInString(term)
	last, _ := utf8.DecodeLastRuneInString(term)
	for offset := 0; offset <= len(text)-len(term); {
		i := strings.Index(text[offset:], term)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(term)
		before, _ := utf8.DecodeLastRuneInString(text[:i])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !(ascii(first) && i > 0 && ascii(before)) && !(ascii(last) && end < len(text) && ascii(after)) {
			return true
		}
		offset = i + len(term)
	}
	return false
}

// Only the current utterance triggers a hint. Conditional and competing senses
// travel intact to the model; the matcher never rewrites the user's question.
func Match(items []Entry, text string) *Context {
	type candidate struct {
		hit    Hit
		length int
		order  int
	}
	candidates := []candidate{}
	for n, e := range items {
		if !e.Enabled {
			continue
		}
		h := Hit{Meaning: e.Meaning, When: e.When, Example: e.Example}
		longest := 0
		for _, t := range e.Triggers {
			if t != "" && contains(text, t) {
				h.Matched = append(h.Matched, t)
				longest = max(longest, utf8.RuneCountInString(t))
			}
		}
		if len(h.Matched) > 0 {
			candidates = append(candidates, candidate{h, longest, n})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].length > candidates[j].length })
	out := &Context{Hits: []Hit{}}
	size := 0
	for _, c := range candidates {
		n := utf8.RuneCountInString(strings.Join(c.hit.Matched, " ") + c.hit.Meaning + c.hit.When + c.hit.Example)
		if len(out.Hits) >= 12 || size+n > 4000 {
			out.Omitted++
			continue
		}
		out.Hits = append(out.Hits, c.hit)
		size += n
	}
	return out
}
