package decisionexecutor

import (
	"regexp"
	"strings"
)

// TTS 只认“能读出来的字”。方括号里的备注、破折号、行尾空格在屏幕上没问题，
// 念出来会变成莫名其妙的停顿或读错，所以送 TTS 前统一清一遍。

var (
	// 括号内容整体去掉，连同括号本身。
	ttsBracketContent = regexp.MustCompile(`（[^（）]*）|【[^【】]*】|〔[^〔〕]*〕|\([^()]*\)|\[[^\[\]]*\]`)
	// 破折号改成停顿，TTS 不认识这个符号。
	ttsDashRuns = regexp.MustCompile(`[—–]+`)
	// 连续空格压成一个。
	ttsMultiSpace = regexp.MustCompile(`[ \t]{2,}`)
	// 标点两侧不留空格。
	ttsSpaceAroundPunct = regexp.MustCompile(`\s*([，。！？；：、,.!?;:])\s*`)
	// 两个汉字之间的空格去掉。
	ttsHanSpace = regexp.MustCompile(`([\p{Han}])\s+([\p{Han}])`)
	// 连续标点合并，避免“，，”这种。
	ttsRepeatedPunct = regexp.MustCompile(`([，,]){2,}`)
)

// sanitizeSpokenText turns model output into something a TTS engine can read:
// no bracketed asides, no dashes, no stray spaces.
func sanitizeSpokenText(text string) string {
	return sanitizeSpokenTextWithPauses(text, true)
}

// sanitizeInteractionSpokenText is the interaction path: it keeps the cleanup
// but never inserts the mainline pause punctuation before TTS.
func sanitizeInteractionSpokenText(text string) string {
	return sanitizeSpokenTextWithPauses(text, false)
}

func sanitizeSpokenTextWithPauses(text string, insertPauses bool) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = ttsBracketContent.ReplaceAllString(text, "")
	text = ttsDashRuns.ReplaceAllString(text, "，")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = ttsMultiSpace.ReplaceAllString(line, " ")
		line = ttsSpaceAroundPunct.ReplaceAllString(line, "$1")
		for ttsHanSpace.MatchString(line) {
			line = ttsHanSpace.ReplaceAllString(line, "$1$2")
		}
		line = ttsRepeatedPunct.ReplaceAllString(line, "$1")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	cleaned := strings.Join(out, "\n")
	if insertPauses {
		return insertSpeechPauses(cleaned)
	}
	return cleaned
}

// isInteractionSpeech keeps mainline pause behavior while removing pause
// punctuation from viewer-facing interaction replies.
func isInteractionSpeech(item decisionItem) bool {
	switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
	case "reply_question", "reply_chat", "reply_follow", "reply_like",
		"complaint", "welcome_named", "welcome_batch", "conversion_signal",
		"warmup", "greeting", "thanks", "order_thanks", "order_fulfillment":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(item.Scene)) {
	case "product_question", "complaint", "greeting", "thanks", "answer":
		return true
	}
	return false
}
