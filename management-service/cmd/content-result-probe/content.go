package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type direction struct {
	ID       string `json:"id"`
	Sequence int    `json:"sequence"`
	Text     string `json:"text"`
}

var listMarker = regexp.MustCompile(`^\s*([0-9]{1,2})([.、．)])(\s*)(.*)$`)

// Model numbering is presentation only. Program identifiers always originate
// from the issued request. Unexpected plain text is retained as one content
// item, never parsed as a relationship declaration or rejected for an ID typo.
func directionItems(raw, requestID string) []direction {
	parts := []string{}
	var current []string
	flush := func() {
		if text := strings.TrimSpace(strings.Join(current, "\n")); text != "" {
			parts = append(parts, text)
		}
		current = nil
	}
	found := false
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		match := listMarker.FindStringSubmatch(line)
		marker := false
		if len(match) > 0 {
			n, _ := strconv.Atoi(match[1])
			marker = n > 0 && n <= 24
			// 1.5/85.5 are decimal business values, not a list prefix.
			if (match[2] == "." || match[2] == "．") && match[3] == "" && len(match[4]) > 0 && match[4][0] >= '0' && match[4][0] <= '9' {
				marker = false
			}
		}
		if marker {
			if found {
				flush()
			} else {
				current = nil
			}
			found = true
			current = append(current, match[4])
		} else if found {
			current = append(current, line)
		}
	}
	if found {
		flush()
	} else if text := strings.TrimSpace(raw); text != "" {
		parts = append(parts, text)
	}
	items := make([]direction, len(parts))
	for i, text := range parts {
		items[i] = direction{ID: fmt.Sprintf("%s/direction-%d", requestID, i+1), Sequence: i + 1, Text: text}
	}
	return items
}

func freezeBinding(source binding) binding {
	source.Products = append([]string(nil), source.Products...)
	source.Facts = append([]string(nil), source.Facts...)
	return source
}
