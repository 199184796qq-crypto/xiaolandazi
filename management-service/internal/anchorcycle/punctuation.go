package anchorcycle

import (
	"strings"
	"unicode"
)

type PunctuationEdit struct {
	Position int    `json:"position"` // Zero-based Unicode offset in model text.
	Before   string `json:"before"`
	After    string `json:"after"`
	Reason   string `json:"reason"`
}

// Formatting only: never add a word, change a number, or invent a sentence.
// Existing ?/! and quoted material are preserved. It deliberately leaves
// ambiguous sentence boundaries alone instead of manufacturing punctuation
// quotas. Scores and macro style measurements continue to use the model text.
func RepairPunctuation(text, style string, maxChars int) (string, []PunctuationEdit) {
	runes := []rune(text)
	out := []rune{}
	edits := []PunctuationEdit{}
	clause := []rune{}
	quoted := false
	aroused := strings.Contains(style, "高亢") || strings.Contains(style, "激动")
	chars := 0
	lastContent := len(runes) - 1
	for lastContent >= 0 && unicode.IsSpace(runes[lastContent]) {
		lastContent--
	}
	for _, r := range runes {
		if !unicode.IsSpace(r) {
			chars++
		}
	}
	canAppend := func() bool { return maxChars <= 0 || chars < maxChars }
	for i, r := range runes {
		if strings.ContainsRune("“”‘’\"", r) {
			quoted = !quoted
		}
		decimal := r == '.' && i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1])
		boundary := !decimal && strings.ContainsRune("，,。；;！？!?\n", r) && !(r == '\n' && i > lastContent)
		if boundary {
			replacement := r
			if !quoted && strings.ContainsRune("，,。", r) {
				if clearQuestion(string(clause)) {
					replacement = '？'
				} else if aroused && r == '。' && clearCallToAction(string(clause)) {
					replacement = '！'
				}
			}
			if replacement != r {
				edits = append(edits, PunctuationEdit{i, string(r), string(replacement), "明确问句或行动呼告的句末标点"})
			}
			out = append(out, replacement)
			clause = nil
		} else {
			out = append(out, r)
			clause = append(clause, r)
		}
	}
	last := len(out) - 1
	for last >= 0 && unicode.IsSpace(out[last]) {
		last--
	}
	if last >= 0 && !quoted && (unicode.IsLetter(out[last]) || unicode.IsDigit(out[last])) && canAppend() {
		mark := '。'
		if clearQuestion(string(clause)) {
			mark = '？'
		} else if aroused && clearCallToAction(string(clause)) {
			mark = '！'
		}
		// Insert before trailing whitespace without otherwise normalizing text.
		out = append(out[:last+1], append([]rune{mark}, out[last+1:]...)...)
		edits = append(edits, PunctuationEdit{last + 1, "", string(mark), "补齐未闭合的段尾标点"})
	}
	return string(out), edits
}

func clearQuestion(clause string) bool {
	clause = strings.TrimSpace(clause)
	for _, ending := range []string{"买好了不", "拍到了不", "选好了不", "听清楚了不", "晓得不", "对不对", "是不是", "划不划算", "够不够用", "好不好吃", "有没有", "听清楚了没", "看明白了没"} {
		if strings.HasSuffix(clause, ending) {
			return true
		}
	}
	return false
}

func clearCallToAction(clause string) bool {
	clause = strings.TrimSpace(clause)
	for _, ending := range []string{"直接拍", "直接拍哟", "直接拍啊", "直接拍就行了", "直接拍就行了啊", "直接拍就行了哟", "赶紧拍", "赶紧去拍", "抓紧拍"} {
		if strings.HasSuffix(clause, ending) {
			return true
		}
	}
	return false
}
