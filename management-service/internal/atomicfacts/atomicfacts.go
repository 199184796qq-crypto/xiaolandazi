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
		if !splittablePrefix(prefix) {
			lines = append(lines, fact.Text)
			continue
		}
		core, _ := splitCoreAndMetadata(body)
		parts := splitTopLevel(core)
		if len(parts) <= 1 {
			lines = append(lines, fact.Text)
			continue
		}
		for _, part := range parts {
			if part == "" {
				continue
			}
			line := part
			if prefix != "" {
				line = prefix + " " + line
			}
			lines = append(lines, line)
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

// Product cards and benefit cards are already one record with many fields;
// splitting their semicolons would turn one product into a dozen "facts".
// Only public policy facts are safe to split by attribute.
func splittablePrefix(prefix string) bool {
	return strings.Contains(prefix, "用户指定公共") || strings.Contains(prefix, "公共")
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

// splitCoreAndMetadata separates the approved fact body from its rewrite /
// forbidden-expression annotations. Semicolons inside those annotations must
// never create new facts.
func splitCoreAndMetadata(body string) (string, string) {
	body = strings.TrimSpace(body)
	for _, marker := range []string{"；安全改写=", "安全改写=", "；禁说=", "禁说=", "【必说】", "【可说】", "【不提】", "【改写】"} {
		if index := strings.Index(body, marker); index >= 0 {
			core := strings.TrimSpace(body[:index])
			tail := strings.TrimSpace(body[index:])
			if tail != "" && !strings.HasPrefix(tail, "；") && !strings.HasPrefix(tail, "【") {
				tail = "；" + tail
			}
			return core, tail
		}
	}
	return body, ""
}
