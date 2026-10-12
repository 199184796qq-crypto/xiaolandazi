package contentstrategy

import (
	"strings"
	"unicode"
)

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

// bridgeTransitionNGrams 是桥话术里纯承接/语气的 2-gram，出现这些不算"预告话题"。
// 这是语言层面的通用词，不依赖任何具体商品，换商品（食品/建材/日化）都适用。
var bridgeTransitionNGrams = map[string]bool{
	"接着": true, "继续": true, "咱们": true, "我们": true, "大家": true,
	"往下": true, "一下": true, "来看": true, "看看": true, "说说": true,
	"再说": true, "还有": true, "回到": true, "拉回": true, "接下来": true,
	"哥哥": true, "姐姐": true, "家人": true, "宝子": true, "老板": true,
	"叔叔": true, "阿姨": true, "老乡": true, "朋友": true,
	"好嘞": true, "好了": true, "就是": true, "这就": true, "都是": true,
	"给您": true, "给大": true, "各位": true,
}

func normalizeSpeechText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsSpace(r) || strings.ContainsRune("，。！？、；：,.!?;:'\"“”‘’（）()《》【】…—～-", r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func charNGrams(text string, n int) map[string]bool {
	runes := []rune(text)
	result := make(map[string]bool, len(runes))
	for i := 0; i+n <= len(runes); i++ {
		result[string(runes[i:i+n])] = true
	}
	return result
}

// BridgePreviewsEntry 判断桥话术是否"预告"了回归点首句要讲的内容（话题重叠）。
// 桥话术应只做纯承接，不应携带回归点要讲的卖点/话题词。
// 通用实现：桥话术去掉承接/语气 2-gram 后，剩下的实词 2-gram 出现在回归点首句里的
// 个数达到阈值（>=2）即视为预告话题，应丢弃桥让主线自然继续。
// 不依赖具体商品词表，换商品都适用。
func BridgePreviewsEntry(bridge, entry string) bool {
	bridge = normalizeSpeechText(bridge)
	entry = normalizeSpeechText(entry)
	if bridge == "" || entry == "" {
		return false
	}
	entryGrams := charNGrams(entry, 2)
	overlap := 0
	for g := range charNGrams(bridge, 2) {
		if bridgeTransitionNGrams[g] {
			continue
		}
		if entryGrams[g] {
			overlap++
		}
	}
	return overlap >= 2
}
