package answerflow

import (
	"github.com/mozillazg/go-pinyin"
	"slices"
	"strings"
	"unicode"

	"livecompanion/management/internal/danmakutest"
)

type correctionEdit struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}
type danmakuLexEntry struct {
	term   string
	pinyin []string
	runes  []rune
}
type danmakuLexicon struct {
	entries []danmakuLexEntry
	known   map[string]bool
}

func correctSearchText(question string, terms []string) (string, []correctionEdit) {
	lex := &danmakuLexicon{known: map[string]bool{}}
	terms = slices.Clone(terms)
	slices.Sort(terms)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if lex.known[term] || len([]rune(term)) < 2 || len([]rune(term)) > 12 || !allHan(term) {
			continue
		}
		lex.known[term] = true
		lex.entries = append(lex.entries, danmakuLexEntry{term: term, pinyin: pinyinSequence(term), runes: []rune(term)})
	}
	text, edits := normalizeDanmakuText(question)
	if text == "" {
		return question, nil
	}
	for range 3 {
		next, edit, ok := fuzzyReplaceOnce(text, lex)
		if !ok {
			break
		}
		text = next
		edits = append(edits, edit)
	}
	return text, edits
}
func normalizeDanmakuText(text string) (string, []correctionEdit) {
	edits := []correctionEdit{}
	original := text
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		case r == 0x3000:
			b.WriteRune(' ')
		case r >= 0x1F300 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF:
			// 表情不进入检索。
		default:
			b.WriteRune(r)
		}
	}
	text = b.String()
	// 去掉中文之间的空格，保留数字/英文之间的空格。
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == ' ' {
			prev, next := rune(0), rune(0)
			if i > 0 {
				prev = runes[i-1]
			}
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if unicode.Is(unicode.Han, prev) || unicode.Is(unicode.Han, next) {
				continue
			}
		}
		if unicode.IsSpace(r) && r != ' ' {
			continue
		}
		out = append(out, r)
	}
	text = strings.TrimSpace(string(out))
	// 合并重复标点：“？？？”→“？”。
	var collapsed strings.Builder
	var prev rune
	for _, r := range text {
		if strings.ContainsRune("？?！!。，,、；;：:", r) && r == prev {
			continue
		}
		collapsed.WriteRune(r)
		prev = r
	}
	text = collapsed.String()
	// 中文数字编号：一号→1号，方便对象名匹配（只处理 1~10）。
	replacements := []struct{ from, to string }{
		{"十号", "10号"}, {"九号", "9号"}, {"八号", "8号"}, {"七号", "7号"},
		{"六号", "6号"}, {"五号", "5号"}, {"四号", "4号"}, {"三号", "3号"},
		{"二号", "2号"}, {"一号", "1号"},
	}
	normalized := []rune(text)
	for i := 0; i < len(normalized); i++ {
		if i > 0 && strings.ContainsRune("零〇一二两三四五六七八九十百千万亿第初0123456789", normalized[i-1]) {
			continue
		}
		for _, item := range replacements {
			size := len([]rune(item.from))
			next := i + size
			if next > len(normalized) || string(normalized[i:next]) != item.from ||
				next < len(normalized) && strings.ContainsRune("0123456789一二三四五六七八九十零", normalized[next]) {
				continue
			}
			normalized = append(append(append([]rune{}, normalized[:i]...), []rune(item.to)...), normalized[next:]...)
			edits = append(edits, correctionEdit{From: item.from, To: item.to, Kind: "normalize"})
			break
		}
	}
	text = string(normalized)
	if text != original {
		// 没有具体替换项时也记录一次清洗，方便页面提示。
		if len(edits) == 0 {
			edits = append(edits, correctionEdit{From: original, To: text, Kind: "normalize"})
		}
	}
	return text, edits
}

// Correct one unambiguous typo without crossing known words or changing
// digits, negation, geography or grammatical particles.
func fuzzyReplaceOnce(text string, lex *danmakuLexicon) (string, correctionEdit, bool) {
	runes := []rune(text)
	protected := make([]bool, len(runes))
	for i := range runes {
		for end := i + 2; end <= min(len(runes), i+12); end++ {
			if lex.known[string(runes[i:end])] {
				for j := i; j < end; j++ {
					protected[j] = true
				}
			}
		}
	}
	sounds := pinyinSequence(text)
	bestIndex, bestWidth, bestScore := -1, 0, 2
	var bestTo string
	for index := range runes {
		for width := 2; width <= min(len(runes)-index, 12); width++ {
			window := string(runes[index : index+width])
			if slices.Contains(protected[index:index+width], true) || !allHan(window) ||
				strings.ContainsAny(window, "不没无未否别禁免的了呢吗吧啊呀嘛") || danmakutest.IsRegion(window) {
				continue
			}
			score, replacement, ambiguous := 2, "", false
			for _, entry := range lex.entries {
				if len(entry.runes) != width || strings.ContainsAny(entry.term, "不没无未否别禁免") {
					continue
				}
				match := 2
				if slices.Equal(entry.pinyin, sounds[index:index+width]) {
					match = 0
				} else if width == 3 && runes[index] == entry.runes[0] &&
					((runes[index+1] == entry.runes[1]) != (runes[index+2] == entry.runes[2])) {
					match = 1
				}
				if match < score {
					score, replacement, ambiguous = match, entry.term, false
				} else if match == score && match < 2 {
					ambiguous = true
				}
			}
			if !ambiguous && replacement != "" && (score < bestScore || score == bestScore && width > bestWidth) {
				bestIndex, bestWidth, bestScore, bestTo = index, width, score, replacement
			}
		}
	}
	if bestIndex < 0 {
		return text, correctionEdit{}, false
	}
	end := bestIndex + bestWidth
	kind := "typo"
	if bestScore == 0 {
		kind = "homophone"
	}
	from := string(runes[bestIndex:end])
	return string(runes[:bestIndex]) + bestTo + string(runes[end:]), correctionEdit{From: from, To: bestTo, Kind: kind}, true
}

// pinyinSequence 取每个字的无声调拼音；非汉字保留原字符。
func pinyinSequence(text string) []string {
	out := []string{}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			values := pinyin.Pinyin(string(r), pinyin.NewArgs())
			if len(values) > 0 && len(values[0]) > 0 {
				out = append(out, values[0][0])
				continue
			}
		}
		out = append(out, string(r))
	}
	return out
}

func allHan(text string) bool {
	for _, r := range text {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return text != ""
}
