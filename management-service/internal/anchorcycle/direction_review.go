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
	r, err := ParseReviewConclusion(raw)
	return r.Decision, err
}

// ReviewConclusion keeps the explanation with the verdict. The caller retains
// the original response separately; extracting a verdict must not discard the
// reason needed by local repair.
type ReviewConclusion struct {
	Decision string
	Reason   string
	Quote    string
}

func reviewLabel(label string) string {
	switch strings.TrimSpace(label) {
	case "通过":
		return "通过"
	case "有问题", "不通过", "违规", "不过":
		return "有问题"
	case "不确定":
		return "不确定"
	}
	return ""
}

func ParseReviewConclusion(raw string) (ReviewConclusion, error) {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
	if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") {
		if line := strings.IndexByte(text, '\n'); line >= 0 {
			text = strings.TrimSpace(text[line+1 : len(text)-3])
		}
	}
	if strings.HasPrefix(text, "{") {
		var payload struct {
			Decision   string `json:"decision"`
			Reason     string `json:"reason"`
			Quote      string `json:"quote"`
			Violations []struct {
				Word   string `json:"词"`
				Rule   string `json:"规则"`
				Why    string `json:"原因"`
				Quote  string `json:"quote"`
				Reason string `json:"reason"`
			} `json:"violations"`
		}
		if json.Unmarshal([]byte(text), &payload) == nil && reviewLabel(payload.Decision) != "" {
			result := ReviewConclusion{Decision: reviewLabel(payload.Decision), Reason: strings.TrimSpace(payload.Reason), Quote: strings.TrimSpace(payload.Quote)}
			// A response cannot grant approval while also listing violations.
			if result.Decision == "通过" && len(payload.Violations) > 0 {
				return ReviewConclusion{}, errors.New("审核结论与问题列表矛盾")
			}
			for _, v := range payload.Violations {
				quote, reason := strings.TrimSpace(v.Word), strings.TrimSpace(v.Why)
				if quote == "" {
					quote = strings.TrimSpace(v.Quote)
				}
				if reason == "" {
					reason = strings.TrimSpace(v.Reason)
				}
				if result.Quote == "" {
					result.Quote = quote
				}
				parts := []string{}
				if quote != "" {
					parts = append(parts, "原句：“"+quote+"”")
				}
				if reason != "" {
					parts = append(parts, reason)
				}
				if v.Rule != "" {
					parts = append(parts, "规则："+v.Rule)
				}
				if len(parts) > 0 {
					if result.Reason != "" {
						result.Reason += "\n"
					}
					result.Reason += strings.Join(parts, "；")
				}
			}
			return result, nil
		}
		return ReviewConclusion{}, errors.New("审核返回未包含可识别的明确结论")
	}
	for _, prefix := range []string{"审核结论：", "审核结论:", "结论：", "结论:"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}
	for _, label := range []string{"不通过", "有问题", "不确定", "违规", "不过", "通过"} {
		value := text
		if strings.HasPrefix(value, "**"+label+"**") {
			value = label + strings.TrimPrefix(value, "**"+label+"**")
		}
		if !strings.HasPrefix(value, label) {
			continue
		}
		rest := strings.TrimPrefix(value, label)
		if rest == "" || strings.ContainsRune("：:。，,；;\n\r", []rune(rest)[0]) || rest == "." || strings.HasPrefix(rest, ". ") {
			return ReviewConclusion{Decision: reviewLabel(label), Reason: strings.TrimLeft(rest, "：:。，,；;. \t\n\r")}, nil
		}
	}
	return ReviewConclusion{}, errors.New("方向审核没有完成约定判断")
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
