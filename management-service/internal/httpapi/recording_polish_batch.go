package httpapi

import (
	"context"
	"log"
	"regexp"
	"strconv"
	"strings"

	"livecompanion/management/internal/anchorcycle"
)

// recordingPolishBatchSize 是一次润色调用里合并的句子数。
// 固定前缀（改写母版 + P0）占单次输入的大头，合并后前缀只付一次。
const recordingPolishBatchSize = 5

var recordingPolishMarker = regexp.MustCompile(`<<\s*(\d+)\s*>>`)

// polishRecordingSentenceBatch 批量润色若干句，保证每一句都有润色结果。
//
// 第一步：带编号标记批量改写，一次调用处理整组，省掉重复的固定前缀。
// 第二步：按标记切回来；标记缺失、为空或数字被改动的句子，逐句再润一次。
// 绝不整组回退原文——那正是之前“好多句子没改”的原因。
func (s *Server) polishRecordingSentenceBatch(ctx context.Context, state *anchorcycle.State, sentences []string) []string {
	out := make([]string, len(sentences))
	copy(out, sentences)
	if len(sentences) == 0 {
		return out
	}

	if len(sentences) > 1 {
		if pieces, ok := s.polishByMarker(ctx, state, sentences); ok {
			for index := range sentences {
				if index < len(pieces) && pieces[index] != "" {
					out[index] = pieces[index]
				}
			}
		}
	}

	// 逐句补齐：批量没覆盖到、或数字不一致的句子单独润色。
	for index := range sentences {
		if out[index] != sentences[index] {
			continue
		}
		polished := strings.TrimSpace(polishRecordingText(ctx, s, state, sentences[index]))
		if polished == "" || !sameNumbers(sentences[index], polished) {
			continue
		}
		out[index] = polished
	}
	return out
}

// polishByMarker 用编号标记批量改写；标记不全就交给逐句兜底。
func (s *Server) polishByMarker(ctx context.Context, state *anchorcycle.State, sentences []string) ([]string, bool) {
	var input strings.Builder
	input.WriteString("下面有 ")
	input.WriteString(strconv.Itoa(len(sentences)))
	input.WriteString(" 句直播口播，请逐句改写成更自然的口语。\n")
	input.WriteString("要求：意思、数字、商品名不变；一句一行；每行必须以原来的编号标记开头，例如 <<1>>；不要合并、不要拆分、不要解释。\n\n")
	for index, sentence := range sentences {
		input.WriteString("<<")
		input.WriteString(strconv.Itoa(index + 1))
		input.WriteString(">>")
		input.WriteString(sentence)
		input.WriteString("\n")
	}
	rewritten := polishRecordingText(ctx, s, state, input.String())
	if strings.TrimSpace(rewritten) == "" {
		return nil, false
	}

	matches := recordingPolishMarker.FindAllStringSubmatchIndex(rewritten, -1)
	if len(matches) == 0 {
		return nil, false
	}
	pieces := make([]string, len(sentences))
	for i, match := range matches {
		number, err := strconv.Atoi(rewritten[match[2]:match[3]])
		if err != nil || number < 1 || number > len(sentences) {
			continue
		}
		end := len(rewritten)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		text := strings.TrimSpace(rewritten[match[1]:end])
		text = strings.TrimSpace(strings.Trim(text, "　 "))
		if text == "" || !sameNumbers(sentences[number-1], text) {
			continue
		}
		pieces[number-1] = text
	}
	filled := 0
	for _, piece := range pieces {
		if piece != "" {
			filled++
		}
	}
	log.Printf("recording stream polish batch markers filled=%d/%d", filled, len(sentences))
	return pieces, true
}

// sameNumbers 要求改写前后出现的数字集合完全一致，避免价格、数量被模型改动。
func sameNumbers(before, after string) bool {
	return numbersKey(before) == numbersKey(after)
}

// numbersKey 把文本里的数字按出现顺序拼成一个可比较的字符串。
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
