package anchorcycle

import (
	"encoding/json"
	"errors"
	"strings"
)

// Receipt identity is assigned by the program; the model returns content only.
type DirectionReviewReceipt struct {
	HookID       string `json:"hook_id"`
	ContentSHA   string `json:"content_sha256"`
	ContextSHA   string `json:"context_sha256"`
	Decision     string `json:"decision"`
	Raw          string `json:"raw_response,omitempty"`
	Error        string `json:"error,omitempty"`
	Model        string `json:"model,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	PromptSHA    string `json:"prompt_sha256,omitempty"`
	ExchangeID   int64  `json:"exchange_id,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
}

func DirectionContentSHA(h AngleHook) string {
	return ProductDigest(struct{ Title, Concern, Entry, Action string }{strings.TrimSpace(h.Title), strings.TrimSpace(h.Concern), strings.TrimSpace(h.Entry), strings.TrimSpace(h.Action)})
}

func ParseDirectionReview(raw string) (string, error) {
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff")), "\n", 2)[0])
	for _, label := range []string{"通过", "有问题", "不确定"} {
		if first == label || strings.HasPrefix(first, label+"：") || strings.HasPrefix(first, label+":") || strings.HasPrefix(first, label+"。") || first == label+"." || strings.HasPrefix(first, label+". ") {
			return label, nil
		}
	}
	// 同义词归一：模型自由输出时可能说「不通过」「违规」，统一映射为「有问题」。
	for _, label := range []string{"不通过", "违规", "不过"} {
		if first == label || strings.HasPrefix(first, label+"：") || strings.HasPrefix(first, label+":") || strings.HasPrefix(first, label+"。") || first == label+"." || strings.HasPrefix(first, label+". ") {
			return "有问题", nil
		}
	}
	// 兼容 JSON 返回：上游母版冲突时模型可能返回 JSON（如 {"decision":"违规"}）。
	// 提取 decision 并把「违规」映射为「有问题」，避免合法判断被当成解析失败。
	if trimmed := strings.TrimSpace(raw); strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var payload struct {
			Decision string `json:"decision"`
		}
		if json.Unmarshal([]byte(trimmed), &payload) == nil {
			switch strings.TrimSpace(payload.Decision) {
			case "通过":
				return "通过", nil
			case "有问题", "违规", "不通过":
				return "有问题", nil
			case "不确定":
				return "不确定", nil
			}
		}
	}
	return "", errors.New("方向审核没有完成约定判断")
}

func (r DirectionReviewReceipt) Completed() bool {
	return r.Error == "" && r.ContentSHA != "" && r.ContextSHA != "" && (r.Decision == "通过" || r.Decision == "有问题" || r.Decision == "不确定")
}

func (r DirectionReviewReceipt) Matches(h AngleHook, contextSHA string) bool {
	return r.Completed() && r.ContentSHA == DirectionContentSHA(h) && r.ContextSHA == contextSHA
}

func ReviewedDirectionCards(cards []AngleHook, receipts []DirectionReviewReceipt, contextSHA string) []AngleHook {
	out := []AngleHook{}
	for _, h := range cards {
		for _, r := range receipts {
			if r.Matches(h, contextSHA) && r.Decision == "通过" {
				out = append(out, h)
				break
			}
		}
	}
	return DistinctDirectionCards(out)
}
