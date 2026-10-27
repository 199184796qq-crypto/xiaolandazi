// Package factguard checks generated speech against the facts: it catches a
// question that assumes something the facts do not support, and it removes
// sentences that bring in numbers, promises or timings the facts never made.
package factguard

import (
	"regexp"
	"strings"

	"livecompanion/management/internal/factbound"
)

// Assertion is what a yes/no question presumes and whether the facts back it.
type Assertion struct {
	Claims  []string
	Missing []string
}

var yesNoPattern = regexp.MustCompile(`是不是|是否是|是.*吗|吗[？?]?$|对不对|对吧|能.*吗|可以.*吗`)

var assertedPhrasePatterns = []*regexp.Regexp{
	regexp.MustCompile(`是不是(.+?)[吗？?]`),
	regexp.MustCompile(`是否是(.+?)[吗？?]`),
	regexp.MustCompile(`(?:能|可以)(.+?)[吗？?]`),
	regexp.MustCompile(`是(.+?)[吗？?]`),
	regexp.MustCompile(`(.{2,12}?)[吗？?]`),
}

// Assertions extracts what a yes/no question presumes: configured promise and
// time words, plus the free-form asserted phrase ("是冷链发货吗").
func Assertions(question, facts string) Assertion {
	question = strings.TrimSpace(question)
	result := Assertion{}
	if !yesNoPattern.MatchString(question) {
		return result
	}
	otherTimeInFacts := false
	for _, word := range factbound.TimeWords() {
		if word != "" && strings.Contains(facts, word) {
			otherTimeInFacts = true
			break
		}
	}
	for _, word := range append(factbound.ClaimWords(), factbound.TimeWords()...) {
		if word == "" || !strings.Contains(question, word) {
			continue
		}
		result.Claims = append(result.Claims, word)
		if strings.Contains(facts, word) {
			continue
		}
		if timeish(word) && otherTimeInFacts {
			// Facts give another time ("第二天上午"): the model can correct the
			// assumption instead of dodging it.
			continue
		}
		result.Missing = append(result.Missing, word)
	}
	// Region questions are already handled by the three-state boundary check: a
	// phrase like "西藏包邮" must stay partial, not become a flat refusal.
	if len(factbound.PlacesIn(question)) == 0 {
		if phrase := assertedPhrase(question); phrase != "" && coverage(phrase, facts) < 0.6 {
			if !(timeish(phrase) && otherTimeInFacts) {
				result.Claims = append(result.Claims, phrase)
				result.Missing = append(result.Missing, phrase)
			}
		}
	}
	return result
}

func assertedPhrase(question string) string {
	for _, pattern := range assertedPhrasePatterns {
		match := pattern.FindStringSubmatch(question)
		if len(match) < 2 {
			continue
		}
		phrase := strings.TrimSpace(match[1])
		phrase = strings.TrimSuffix(phrase, "吗")
		if len([]rune(phrase)) >= 2 {
			return phrase
		}
	}
	return ""
}

// coverage is the share of the phrase's character bigrams present in the facts.
func coverage(phrase, facts string) float64 {
	runes := []rune(phrase)
	if len(runes) < 2 {
		return 0
	}
	total, hits := 0, 0
	for i := 0; i+1 < len(runes); i++ {
		total++
		if strings.Contains(facts, string(runes[i:i+2])) {
			hits++
		}
	}
	return float64(hits) / float64(total)
}

func isTimeWord(word string) bool {
	for _, item := range factbound.TimeWords() {
		if item == word {
			return true
		}
	}
	return false
}

func timeish(word string) bool {
	for _, item := range factbound.TimeWords() {
		if item != "" && strings.Contains(word, item) {
			return true
		}
	}
	return false
}

var numberPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// numeralUnitPattern catches Chinese quantities such as "三天" or "两斤" that a
// digit-only check would miss.
var numeralUnitPattern = regexp.MustCompile(`[0-9０-９一二三四五六七八九十两]+(?:天|小时|分钟|个月|年|斤|公斤|千克|克|件|箱|袋|盒|瓶|套|元|块|折|人|份)`)

// Audit removes sentences that add numbers, promises or timings the facts never
// contain. It returns the cleaned text and what was dropped.
func Audit(text, facts string) (string, []string) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	var kept, dropped []string
	for _, sentence := range splitSentences(text) {
		if strings.TrimSpace(sentence) == "" {
			continue
		}
		if unsupported(sentence, facts) {
			dropped = append(dropped, strings.TrimSpace(sentence))
			continue
		}
		kept = append(kept, sentence)
	}
	return strings.TrimSpace(strings.Join(kept, "")), dropped
}

func unsupported(sentence, facts string) bool {
	for _, number := range numberPattern.FindAllString(sentence, -1) {
		if !strings.Contains(facts, number) {
			return true
		}
	}
	for _, quantity := range numeralUnitPattern.FindAllString(sentence, -1) {
		if !strings.Contains(facts, quantity) {
			return true
		}
	}
	for _, word := range append(factbound.ClaimWords(), factbound.TimeWords()...) {
		if word != "" && strings.Contains(sentence, word) && !strings.Contains(facts, word) {
			return true
		}
	}
	return false
}

func splitSentences(text string) []string {
	var out []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		switch r {
		case '。', '！', '？', '；', '!', '?', ';', '\n':
			out = append(out, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}
