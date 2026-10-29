package decisionexecutor

import "strings"

// normalizeNameListSeparators keeps multiple addressed names separated by the
// Chinese enumeration comma, even when the model writes a normal comma between
// them.
func normalizeNameListSeparators(text string, names []string) string {
	text = strings.TrimSpace(text)
	if len(names) < 2 || text == "" {
		return text
	}
	for i := 0; i+1 < len(names); i++ {
		left := strings.TrimSpace(names[i])
		right := strings.TrimSpace(names[i+1])
		if left == "" || right == "" {
			continue
		}
		for _, sep := range []string{"，", ",", "、", " ", "\n"} {
			text = strings.ReplaceAll(text, left+sep+right, left+"、"+right)
		}
	}
	return text
}

// ensureAddressPunctuation inserts a comma after the frozen address when the
// model runs the next phrase straight into it, so TTS hears a break instead of
// one long run-on ("可爱噜健康一生老乡们来了").
func ensureAddressPunctuation(text, address string) string {
	text = strings.TrimSpace(text)
	address = strings.TrimSpace(address)
	if text == "" || address == "" {
		return text
	}
	runes := []rune(text)
	target := []rune(address)
	if len(target) > len(runes) {
		return text
	}
	for i := 0; i+len(target) <= len(runes); i++ {
		if string(runes[i:i+len(target)]) != address {
			continue
		}
		next := i + len(target)
		if next >= len(runes) {
			return text
		}
		switch runes[next] {
		case '，', '。', '！', '？', '、', '；', '：', ' ', '\n', '~', '～', '…':
			return text
		case '的', '了', '啊', '呀', '吧', '吗', '呢', '么', '着', '过':
			// 称呼后直接跟助词时不能断句（「等等的点赞」）。
			return text
		}
		out := make([]rune, 0, len(runes)+1)
		out = append(out, runes[:next]...)
		out = append(out, '，')
		out = append(out, runes[next:]...)
		return string(out)
	}
	return text
}
