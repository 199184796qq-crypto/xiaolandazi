package decisionexecutor

import (
	"regexp"
	"strings"
)

var (
	answerPositivePattern = regexp.MustCompile(`选|拍|买|推荐|就行|就够|合适|可以|包邮|空运|支持`)
	answerNegativePattern = regexp.MustCompile(`没确认|没核实|说不好|不敢|没底|没谱|不清楚|不确定|没数`)
	answerPersonPattern   = regexp.MustCompile(`[0-9一二两三四五六七八九十]+\s*(?:个)?人`)
	answerValuePattern    = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?\s*(?:g|G|克|元|天|号|个)|[0-9]+号`)
)

// arbitrateAnswerClaims is a program-side fact arbitration, not a general
// contradiction filter. When one answer contains both a positive recommendation
// and a denial, it keeps the side supported by the selected facts:
// supported recommendation -> drop denial; unsupported recommendation -> drop it
// and let the caller fall back to the boundary/handoff.
func arbitrateAnswerClaims(answer, question, facts string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" || facts == "" {
		return answer
	}
	parts := splitAnswerSentences(answer)
	var positive, negative []string
	for _, part := range parts {
		if answerNegativePattern.MatchString(part) {
			negative = append(negative, part)
			continue
		}
		if answerPositivePattern.MatchString(part) && mentionsAnswerSubject(part, question) {
			positive = append(positive, part)
		}
	}
	if len(positive) == 0 || len(negative) == 0 {
		return answer
	}
	supported := false
	for _, sentence := range positive {
		if answerSentenceSupported(sentence, facts) {
			supported = true
			break
		}
	}
	if !supported {
		// The recommendation has no fact relation (for example "2人适合450g"
		// when facts only say "450g"). Drop it; caller will use the boundary.
		return ""
	}
	var kept []string
	for _, part := range parts {
		if answerNegativePattern.MatchString(part) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "。") + "。"
}

func splitAnswerSentences(value string) []string {
	var out []string
	start := 0
	for i, r := range value {
		switch r {
		case '。', '！', '？', '!', '?':
			if s := strings.TrimSpace(value[start:i]); s != "" {
				out = append(out, s)
			}
			start = i + len(string(r))
		}
	}
	if s := strings.TrimSpace(value[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

func mentionsAnswerSubject(sentence, question string) bool {
	if answerPersonPattern.MatchString(question) && answerPersonPattern.MatchString(sentence) {
		return true
	}
	for _, token := range []string{"上海", "北京", "成都", "新疆", "海南", "广州", "深圳", "杭州", "武汉", "重庆"} {
		if strings.Contains(question, token) && strings.Contains(sentence, token) {
			return true
		}
	}
	return answerValuePattern.MatchString(sentence)
}

func answerSentenceSupported(sentence, facts string) bool {
	facts = strings.ReplaceAll(facts, " ", "")
	// Person recommendations need an explicit serving-size relation, not just a
	// spec value. "450g exists" does not prove "450g is right for 2 people".
	if person := answerPersonPattern.FindString(sentence); person != "" {
		person = strings.ReplaceAll(person, " ", "")
		if !strings.Contains(facts, person) && !strings.Contains(facts, "适合"+person) && !strings.Contains(facts, person+"适合") {
			return false
		}
	}
	for _, token := range []string{"上海", "北京", "成都", "新疆", "海南", "广州", "深圳", "杭州", "武汉", "重庆"} {
		if strings.Contains(sentence, token) && !strings.Contains(facts, token) {
			return false
		}
	}
	for _, value := range answerValuePattern.FindAllString(sentence, -1) {
		value = strings.ReplaceAll(value, " ", "")
		if value != "" && !strings.Contains(facts, value) {
			return false
		}
	}
	return true
}
