package contentstrategy

import "strings"

// SpeechUnits retains every spoken character, including punctuation and numbers.
// Only transport whitespace around a complete sentence is ignored for equality.
func SpeechUnits(text string) []string {
	var units []string
	var b strings.Builder
	flush := func() {
		if value := strings.TrimSpace(b.String()); value != "" {
			units = append(units, value)
		}
		b.Reset()
	}
	for _, r := range text {
		if r == '\n' || r == '\r' {
			flush()
			continue
		}
		b.WriteRune(r)
		if strings.ContainsRune("。！？!?", r) {
			flush()
		}
	}
	flush()
	return units
}

func SameSpeech(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && a == b
}

// RemoveConsecutiveSpeech runs before TTS; it never estimates audio cut positions.
// An explicit verbatim/repeat task keeps its operator-specified text intact.
func RemoveConsecutiveSpeech(text string, allowRepeat bool) (string, int) {
	text = strings.TrimSpace(text)
	if allowRepeat {
		return text, 0
	}
	paragraphs := strings.Split(text, "\n")
	kept := make([]string, 0, len(paragraphs))
	removed := 0
	previous := ""
	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			kept = append(kept, paragraph)
			continue
		}
		if SameSpeech(previous, paragraph) {
			removed++
			continue
		}
		previous = paragraph
		kept = append(kept, paragraph)
	}
	text = strings.Join(kept, "\n")
	// Preserve original spacing when no adjacent sentence needs removal.
	var result strings.Builder
	previous = ""
	duplicates := 0
	start := 0
	write := func(end int) {
		unit := text[start:end]
		start = end
		if strings.TrimSpace(unit) == "" {
			result.WriteString(unit)
			return
		}
		if SameSpeech(previous, unit) {
			duplicates++
			return
		}
		result.WriteString(unit)
		previous = unit
	}
	for at, r := range text {
		if strings.ContainsRune("。！？!?\n\r", r) {
			write(at + len(string(r)))
		}
	}
	if start < len(text) {
		write(len(text))
	}
	if duplicates > 0 {
		text = result.String()
	}
	return text, removed + duplicates
}

// BridgeDuplicatesAdjacent checks only the immediate answer tail and return head.
func BridgeDuplicatesAdjacent(bridge, reply, entry string, allowRepeat bool) bool {
	if allowRepeat {
		return false
	}
	left, right := SpeechUnits(reply), SpeechUnits(entry)
	return (len(left) > 0 && SameSpeech(bridge, left[len(left)-1])) ||
		(len(right) > 0 && SameSpeech(bridge, right[0]))
}

// RemoveRepeatedEntry operates on the next text before its voice is created.
// It compares the entire paragraph or the immediately adjacent sentence only.
func RemoveRepeatedEntry(previous, text string, allowRepeat bool) (string, int) {
	text = strings.TrimSpace(text)
	if allowRepeat {
		return text, 0
	}
	if SameSpeech(previous, text) {
		return "", 1
	}
	left := SpeechUnits(previous)
	if len(left) == 0 {
		return text, 0
	}
	removed := 0
	for {
		right := SpeechUnits(text)
		if len(right) == 0 || !SameSpeech(left[len(left)-1], right[0]) {
			return text, removed
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, right[0]))
		removed++
	}
}
