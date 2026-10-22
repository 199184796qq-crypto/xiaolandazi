package httpapi

import (
	"context"
	"log"
	"strings"

	"livecompanion/management/internal/anchorcycle"
)

// recordingPolishBatchSize 是一次润色调用里合并的句子数。
// 固定前缀（改写母版 + P0）占单次输入的大头，合并后前缀只付一次，
// 但返回必须逐行对齐，否则会串句。
const recordingPolishBatchSize = 5

// polishRecordingSentenceBatch 把若干句合并成一次润色调用，返回逐句结果。
//
// 对齐规则：返回行数必须与输入句数一致，任何一行缺失或多余都整组回退原文。
// 数字规则：每句里的数字集合必须与原句一致，不一致的那一句回退原文。
// 这两条是合并润色的安全边界，宁可用原文也不能串位或改数。
func (s *Server) polishRecordingSentenceBatch(ctx context.Context, state *anchorcycle.State, sentences []string) []string {
	out := make([]string, len(sentences))
	for index, sentence := range sentences {
		out[index] = sentence
	}
	if len(sentences) == 0 {
		return out
	}
	if len(sentences) == 1 {
		if polished := polishRecordingText(ctx, s, state, sentences[0]); strings.TrimSpace(polished) != "" {
			out[0] = polished
		}
		return out
	}

	// 把 5 句当成一整段交给模型改写，回来按句号切回 5 句。
	// 不要求模型按行输出——它本来不是按行工作的，强行要求行数只会失败。
	passage := strings.Join(sentences, "")
	rewritten := polishRecordingText(ctx, s, state, passage)
	if strings.TrimSpace(rewritten) == "" {
		return out
	}

	parts := splitRecordingSentences(rewritten)
	if len(parts) != len(sentences) {
		log.Printf("recording stream polish batch sentence count changed want=%d got=%d, fallback to script", len(sentences), len(parts))
		return out
	}
	for index := range sentences {
		candidate := strings.TrimSpace(parts[index])
		if candidate == "" || !sameNumbers(sentences[index], candidate) {
			continue
		}
		out[index] = candidate
	}
	return out
}

// splitPolishedLines 把模型返回按行拆开，丢掉编号、空行和解释行。
func splitPolishedLines(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "-*·• 　")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 去掉「1.」「1、」「(1)」这类编号前缀。
		trimmed := line
		for len(trimmed) > 0 && (trimmed[0] >= '0' && trimmed[0] <= '9') {
			trimmed = trimmed[1:]
		}
		trimmed = strings.TrimLeft(trimmed, ".、)）:：")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			line = trimmed
		}
		lines = append(lines, line)
	}
	return lines
}

// sameNumbers 要求改写前后出现的数字集合完全一致，避免价格、数量被模型改动。
func sameNumbers(before, after string) bool {
	return numbersKey(before) == numbersKey(after)
}

func numbersKey(text string) string {
	var digits []rune
	var groups []string
	flush := func() {
		if len(digits) > 0 {
			groups = append(groups, string(digits))
			digits = digits[:0]
		}
	}
	for _, r := range text {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
			continue
		}
		flush()
	}
	flush()
	return strings.Join(groups, "|")
}
