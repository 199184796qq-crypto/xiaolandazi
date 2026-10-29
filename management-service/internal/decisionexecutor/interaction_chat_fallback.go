package decisionexecutor

import "strings"

// lightChatFallback handles short social/emotional lines whose purpose the
// intent model left empty. It acknowledges first; a product tie-in is optional
// and never claims a health benefit.
func lightChatFallback(question, address string) (string, bool) {
	q := strings.TrimSpace(question)
	switch {
	case strings.Contains(q, "累") || strings.Contains(q, "困") || strings.Contains(q, "乏"):
		return address + "，累了就先歇会儿，别硬撑；想吃点啥放松一下，整点鸭子配点小酒，解解馋也行。", true
	case q == "在吗" || q == "在不在" || q == "主播在吗":
		return address + "，在呢，你说。", true
	case strings.Contains(q, "你好") || strings.Contains(q, "哈喽") || strings.Contains(q, "嗨"):
		return address + "，你好呀，想了解啥直接说。", true
	}
	return "", false
}
