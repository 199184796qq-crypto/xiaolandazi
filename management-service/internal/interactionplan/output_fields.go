package interactionplan

import (
	"errors"
	"strconv"
	"strings"
)

// IntentAlternative is non-spoken metadata; only Body enters the voice chain.
type IntentAlternative struct {
	Meaning    string `json:"meaning"`
	Confidence *int   `json:"confidence,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Intent scores and suggestions are optional, non-spoken metadata. A malformed
// hint cannot invalidate a complete answer or manufacture certainty.
func parseIntentLines(value string) ([]IntentAlternative, error) {
	result, _ := parseIntentMetadata(value)
	return result, nil
}

func parseIntentMetadata(value string) ([]IntentAlternative, []ParseWarning) {
	var result []IntentAlternative
	var warnings []ParseWarning
	warn := func(code, field string, line int) { warnings = append(warnings, ParseWarning{code, field, line}) }
	if strings.TrimSpace(value) == "" {
		warn("missing_intent_metadata", "意图", 0)
		return nil, warnings
	}
	last := 101
	for i, line := range strings.Split(value, "\n") {
		if len(result) == 3 {
			warn("extra_intent_metadata", "意图", i+1)
			break
		}
		parts := strings.Split(strings.ReplaceAll(line, "｜", "|"), "|")
		entry := IntentAlternative{Meaning: strings.TrimSpace(parts[0])}
		if entry.Meaning == "" {
			warn("empty_intent_metadata", "意图", i+1)
			continue
		}
		confidenceSeen, confidenceInvalid, suggestionSeen, suggestionConflict := false, false, false, false
		for _, part := range parts[1:] {
			if strings.TrimSpace(part) == "" {
				warn("empty_auxiliary_separator", "意图", i+1)
				continue
			}
			key, val, ok := splitKVLine(part)
			if !ok {
				warn("malformed_intent_auxiliary", "意图", i+1)
				continue
			}
			switch key {
			case "置信度":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 || n > 100 {
					confidenceInvalid = true
					warn("invalid_confidence", key, i+1)
				} else if confidenceSeen && (entry.Confidence == nil || *entry.Confidence != n) {
					confidenceInvalid = true
					warn("conflicting_confidence", key, i+1)
				} else if confidenceSeen {
					warn("identical_auxiliary_repeated", key, i+1)
				} else {
					entry.Confidence = &n
				}
				confidenceSeen = true
			case "建议":
				if suggestionSeen && entry.Suggestion != val {
					suggestionConflict = true
					warn("conflicting_suggestion", key, i+1)
				} else if suggestionSeen {
					warn("identical_auxiliary_repeated", key, i+1)
				} else {
					entry.Suggestion = val
				}
				suggestionSeen = true
			default:
				warn("unknown_intent_auxiliary", "意图", i+1)
			}
		}
		if confidenceInvalid {
			entry.Confidence = nil
		}
		if suggestionConflict {
			entry.Suggestion = ""
		}
		if entry.Confidence == nil {
			// A plain five-field intent has no confidence contract. Only warn
			// when a legacy auxiliary section attempted to supply metadata.
			if len(parts) > 1 {
				warn("unknown_confidence", "置信度", i+1)
			}
		} else {
			if *entry.Confidence > last {
				warn("intent_order", "意图", i+1)
			}
			last = *entry.Confidence
		}
		result = append(result, entry)
	}
	return result, warnings
}

func splitKVLine(line string) (string, string, bool) {
	pos := strings.IndexAny(line, "：:")
	if pos <= 0 {
		return "", "", false
	}
	width := 1
	if strings.HasPrefix(line[pos:], "：") {
		width = len("：")
	}
	return strings.TrimSpace(line[:pos]), strings.TrimSpace(line[pos+width:]), true
}

func isInteractionField(key string) bool {
	switch key {
	case "意图", "类型", "优先程度", "优先原因", "歧义", "候选", "澄清句", "话术要求", "话术", "回答", "对话关系", "继续对话":
		return true
	}
	return false
}

func bodyField(key string) bool { return key == "话术" || key == "回答" }

// A literal formatting instruction is an empty placeholder, not speech. Only
// this explicit marker (optionally bracketed) is normalized; ordinary answers
// such as 无 or a sentence containing 留空 retain their original meaning.
func spokenFieldValue(value string) string {
	value = strings.TrimSpace(value)
	if strings.TrimSpace(strings.Trim(value, "（）()[]【】")) == "留空" {
		return ""
	}
	return value
}

// The two spoken values decide which branch is usable. Analysis metadata is
// advisory: missing/invalid ambiguity or candidates cannot discard one unique
// non-empty spoken value. Both non-empty values still need a clear selection.
func spokenValue(kv map[string]string) (string, string, error) {
	body := spokenFieldValue(kv["话术"])
	old := spokenFieldValue(kv["回答"])
	if body != "" && old != "" && body != old {
		return "", "", errors.New("互动结果存在不同的正文值")
	}
	if body == "" {
		body = old
	}
	clarify := spokenFieldValue(kv["澄清句"])
	switch {
	case body != "" && clarify != "":
		switch strings.TrimSpace(kv["歧义"]) {
		case "是":
			return "澄清句", clarify, nil
		case "否":
			return "话术", body, nil
		default:
			return "", "", errors.New("互动话术和澄清句同时有值，无法确认采用哪一个")
		}
	case body != "":
		return "话术", body, nil
	case clarify != "":
		return "澄清句", clarify, nil
	default:
		return "", "", nil
	}
}

func validateContentFields(kv map[string]string) error {
	_, _, err := spokenValue(kv)
	return err
}
