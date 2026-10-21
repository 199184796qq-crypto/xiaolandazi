package decisionexecutor

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

// TTS 前的句级 P0 处理。
//
// 不整段重生成：先把正文按句切开，只把命中 P0 的那一句回喂模型，
// 用很短的四层提示词要 2 个备选，程序挑第一个仍不含红线的回填；
// 两个都不行就丢掉这一句，其余句子原样组装后送 TTS。
// 这样 token 成本只和命中句数相关，不会因为一句违规重写整段。

const ttsP0RewriteSystem = `# 必说
只改写下面这一句直播口播，保留原句的沟通任务和事实。
不改句子结构、不换主题、不换商品链接、不新增数字、不补充原文没有的信息。

# 可说
原句就是唯一事实来源；只换说法，不换内容。

# 不提（这些词一个都不能出现在改法里）
{{NOT_SAY}}

# 改写（照着换）
- 最好 / 最佳 / 最优 → 很好 / 很不错 / 挺好
- 最低价 / 最便宜 / 最优惠 → 价格更低 / 更便宜 / 更划算
- 最高 / 最大 / 最多 → 很高 / 很大 / 很多
- 顶级 / 极致 / 顶尖 → 很好 / 不错 / 到位
- 第一 / 唯一 / 独家 → 少有的 / 比较特别
- 100% / 绝对 / 零风险 → 基本 / 通常 / 几乎
- 保证 / 确保 → 可以做到 / 一般都能
保持口播语气，长度不超过原句。
只输出 JSON：{"备选":["改写1","改写2"]}`

type ttsP0RewriteDoc struct {
	Candidates []string `json:"备选"`
}

// splitTTSSentences 按中文句末标点和换行切句，保留标点与换行。
func splitTTSSentences(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	runes := []rune(text)
	out := make([]string, 0, 8)
	start := 0
	for i, r := range runes {
		if r != '。' && r != '！' && r != '？' && r != '!' && r != '?' && r != '；' && r != ';' && r != '\n' {
			continue
		}
		part := string(runes[start : i+1])
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		} else if strings.Contains(part, "\n") {
			out = append(out, "\n")
		}
		start = i + 1
	}
	if start < len(runes) {
		part := string(runes[start:])
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

func ttsP0RewriteCandidates(raw string) []string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil
	}
	var doc ttsP0RewriteDoc
	if json.Unmarshal([]byte(raw[start:end+1]), &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Candidates))
	for _, item := range doc.Candidates {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func ttsP0UniqueHits(hits []contentguard.Hit) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		text := strings.TrimSpace(hit.Text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}

var ttsP0NumberPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// ttsP0LinkMention 取出“2号链接”这类链接标记；没有返回空串。
func ttsP0LinkMention(text string) string {
	const suffix = "号链接"
	if idx := strings.Index(text, suffix); idx > 0 {
		start := idx
		for start > 0 {
			r := rune(text[start-1])
			if r < '0' || r > '9' {
				break
			}
			start--
		}
		if start < idx {
			return text[start : idx+len(suffix)]
		}
	}
	return ""
}

func ttsP0HasNewNumber(original, candidate string) bool {
	existing := map[string]bool{}
	for _, number := range ttsP0NumberPattern.FindAllString(original, -1) {
		existing[number] = true
	}
	for _, number := range ttsP0NumberPattern.FindAllString(candidate, -1) {
		if !existing[number] {
			return true
		}
	}
	return false
}

// ttsP0PickCandidate 选第一个仍不含 P0、且没有换链接/新增数字的备选。
func ttsP0PickCandidate(candidates []string, original string, policy *contentguard.Policy) string {
	if policy == nil {
		return ""
	}
	originalLink := ttsP0LinkMention(original)
	for _, item := range candidates {
		if len(policy.Scan(item, "all").Hits) != 0 {
			continue
		}
		candidateLink := ttsP0LinkMention(item)
		if originalLink == "" && candidateLink != "" {
			continue
		}
		if originalLink != "" && candidateLink != originalLink {
			continue
		}
		if ttsP0HasNewNumber(original, item) {
			continue
		}
		return item
	}
	return ""
}

func ttsP0KeepSentencePunctuation(original, rewritten string) string {
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" {
		return ""
	}
	trimmed := strings.TrimRight(original, "。！？!?；;\n")
	if trimmed == original || len([]rune(rewritten)) == 0 {
		return rewritten
	}
	last := []rune(rewritten)[len([]rune(rewritten))-1]
	if strings.ContainsRune("。！？!?；;", last) {
		return rewritten
	}
	originalRunes := []rune(original)
	return rewritten + string(originalRunes[len(originalRunes)-1])
}

func (w *Worker) rewriteOneTTSSentence(
	ctx context.Context,
	session model.LiveRuntimeSession,
	sentence string,
	hits []contentguard.Hit,
	policy *contentguard.Policy,
) string {
	if w == nil || w.agent == nil || policy == nil {
		return ""
	}
	notSay := strings.Join(ttsP0UniqueHits(hits), "、")
	if notSay == "" {
		return ""
	}
	system := strings.Replace(ttsP0RewriteSystem, "{{NOT_SAY}}", notSay, 1)
	user := "原句：" + strings.TrimSpace(sentence) + "\n本句违规词：" + notSay
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	response, err := w.completeInteractionAnswer(callCtx, session, agentgateway.Request{
		Stage:          "interaction_p0_sentence_rewrite",
		Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:      160,
		ResponseFormat: agentgateway.ResponseJSON,
		EnableThinking: false,
		Timeout:        8 * time.Second,
	})
	if err != nil {
		return ""
	}
	picked := ttsP0PickCandidate(ttsP0RewriteCandidates(response.Text), sentence, policy)
	if picked == "" {
		return ""
	}
	picked = ttsP0KeepSentencePunctuation(sentence, picked)
	if len([]rune(picked)) > len([]rune(strings.TrimSpace(sentence)))*2+20 {
		return ""
	}
	return picked
}

// rewriteTTSP0Sentences 返回处理后的正文、改写句数、丢弃句数。
func (w *Worker) rewriteTTSP0Sentences(
	ctx context.Context,
	session model.LiveRuntimeSession,
	text string,
) (string, int, int) {
	p0MD, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		return text, 0, 0
	}
	policy, err := contentguard.Compile(string(p0MD))
	if err != nil || policy == nil {
		return text, 0, 0
	}
	sentences := splitTTSSentences(text)
	if len(sentences) == 0 {
		return text, 0, 0
	}
	out := make([]string, 0, len(sentences))
	rewritten, dropped := 0, 0
	for _, sentence := range sentences {
		if strings.TrimSpace(sentence) == "\n" {
			out = append(out, sentence)
			continue
		}
		hits := policy.Scan(sentence, "all").Hits
		if len(hits) == 0 {
			out = append(out, sentence)
			continue
		}
		replacement := w.rewriteOneTTSSentence(ctx, session, sentence, hits, policy)
		if replacement == "" {
			dropped++
			continue
		}
		out = append(out, replacement)
		rewritten++
	}
	result := strings.TrimSpace(strings.Join(out, ""))
	if result == "" {
		fallback := "这个我这边没有依据，不能乱说，咱们以直播间页面为准。"
		if len(policy.Scan(fallback, "all").Hits) == 0 {
			return fallback, rewritten, dropped
		}
	}
	return result, rewritten, dropped
}
