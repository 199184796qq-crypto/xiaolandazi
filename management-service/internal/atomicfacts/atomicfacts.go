// Package atomicfacts turns a multi-attribute fact package into one-attribute
// fact lines before it is used for selling-point pools, cards or answers.
package atomicfacts

import (
	"strings"

	"livecompanion/management/internal/contentstrategy"
)

// Fact is one rendered atomic fact line. IDs stay positional (F001, F002...)
// so existing consumers of contentstrategy.Facts keep working.
type Fact struct {
	ID   string
	Text string
}

// Build splits top-level clauses on Chinese/ASCII semicolons. Commas are kept:
// conditions such as "本地不抽真空，外地真空包装" are one packaging attribute
// with two branches and must not be broken apart blindly.
func Build(text string) (string, []Fact) {
	source := contentstrategy.Facts(text)
	lines := make([]string, 0, len(source)+4)
	for _, fact := range source {
		prefix, body := splitPrefix(fact.Text)
		parts := splitTopLevel(body)
		if len(parts) <= 1 {
			lines = append(lines, fact.Text)
			continue
		}
		for _, part := range parts {
			if part == "" {
				continue
			}
			if prefix == "" {
				lines = append(lines, part)
			} else {
				lines = append(lines, prefix+" "+part)
			}
		}
	}
	rendered := strings.Join(lines, "\n")
	parsed := contentstrategy.Facts(rendered)
	out := make([]Fact, 0, len(parsed))
	for _, fact := range parsed {
		out = append(out, Fact{ID: fact.ID, Text: fact.Text})
	}
	return rendered, out
}

func splitPrefix(text string) (string, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "- [") {
		return "", text
	}
	end := strings.Index(text, "]")
	if end < 0 {
		return "", text
	}
	return text[:end+1], strings.TrimSpace(text[end+1:])
}

func splitTopLevel(body string) []string {
	body = strings.ReplaceAll(body, "；", ";")
	raw := strings.Split(body, ";")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
