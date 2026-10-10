package audioout

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

// BridgeDuplicatesAdjacent checks only the immediate answer tail and return head.
func BridgeDuplicatesAdjacent(bridge, reply, entry string, allowRepeat bool) bool {
	if allowRepeat {
		return false
	}
	left, right := SpeechUnits(reply), SpeechUnits(entry)
	return (len(left) > 0 && SameSpeech(bridge, left[len(left)-1])) ||
		(len(right) > 0 && SameSpeech(bridge, right[0]))
}
