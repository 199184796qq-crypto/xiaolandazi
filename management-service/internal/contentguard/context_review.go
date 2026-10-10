package contentguard

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type ContextItem struct {
	ID       string `json:"hit_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}
type ContextOutput struct {
	Items            []ContextItem `json:"items"`
	InstructionLeak  *bool         `json:"instruction_leak"`
	InstructionQuote string        `json:"instruction_quote"`
}
type ContextReview struct {
	TextSHA     string        `json:"text_sha256"`
	PromptSHA   string        `json:"prompt_sha256"`
	Provider    string        `json:"provider"`
	Model       string        `json:"model"`
	Raw         string        `json:"raw_response"`
	LexicalHits []Hit         `json:"lexical_hits"`
	Output      ContextOutput `json:"output"`
	Error       string        `json:"error,omitempty"`
	LatencyMS   int64         `json:"latency_ms"`
}

// Hard matches never enter model review. A contextual match is still pending
// until every original occurrence has a structurally valid, bound decision.
func NeedsContextReview(r Report) bool {
	if r.Passed || r.Error != "" || len(r.Hits) == 0 || r.ContextReview != nil {
		return false
	}
	for _, h := range r.Hits {
		if h.ReviewMode != "semantic" {
			return false
		}
	}
	return true
}
func (p *Policy) ContextItems(r Report) ([]map[string]any, error) {
	if r.SHA256 != p.SHA256 || !NeedsContextReview(r) {
		return nil, errors.New("候选不是本规则的待语境复核结果")
	}
	rules := map[string]Rule{}
	for _, rule := range p.Data.Rules {
		rules[rule.ID] = rule
	}
	items := make([]map[string]any, 0, len(r.Hits))
	for i, hit := range r.Hits {
		rule, ok := rules[hit.RuleID]
		if !ok || rule.ReviewMode != "semantic" {
			return nil, errors.New("待复核命中没有对应语境规则")
		}
		items = append(items, map[string]any{"hit_id": fmt.Sprintf("h%d", i+1), "hit": hit, "rule": rule})
	}
	return items, nil
}

func ResolveContext(r Report, text string, review ContextReview) (Report, error) {
	if !NeedsContextReview(r) {
		return r, errors.New("不能由模型改写硬禁或已完成的审核")
	}
	review.TextSHA = fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	review.LexicalHits = append([]Hit(nil), r.Hits...)
	body := strings.TrimSpace(review.Raw)
	if strings.HasPrefix(body, "```") {
		if first := strings.IndexByte(body, '\n'); first >= 0 && strings.HasSuffix(body, "```") {
			body = strings.TrimSpace(body[first+1 : len(body)-3])
		}
	}
	decoder := json.NewDecoder(strings.NewReader(body))
	var output ContextOutput
	if err := decoder.Decode(&output); err != nil {
		return r, errors.New("语境复核JSON无效")
	}
	if decoder.Decode(new(any)) != io.EOF || len(output.Items) != len(r.Hits) || output.InstructionLeak == nil {
		return r, errors.New("语境复核缺少全部命中或指导泄漏结果")
	}
	byID := map[string]ContextItem{}
	for _, item := range output.Items {
		item.ID = strings.TrimSpace(item.ID)
		item.Decision = strings.ToLower(strings.TrimSpace(item.Decision))
		item.Reason = strings.TrimSpace(item.Reason)
		switch item.Decision {
		case "allow":
			item.Decision = "pass"
		case "reject":
			item.Decision = "block"
		}
		if item.Decision != "pass" && item.Decision != "block" && item.Decision != "evidence_required" {
			return r, errors.New("语境复核决定无效")
		}
		if item.Reason == "" || len(item.Reason) > 2400 {
			return r, errors.New("语境复核理由无效")
		}
		if _, exists := byID[item.ID]; exists {
			return r, errors.New("语境复核命中重复")
		}
		byID[item.ID] = item
	}
	remaining := []Hit{}
	for i, hit := range r.Hits {
		item, ok := byID[fmt.Sprintf("h%d", i+1)]
		if !ok {
			return r, errors.New("语境复核返回未知命中")
		}
		output.Items[i] = item
		if item.Decision != "pass" {
			remaining = append(remaining, hit)
		}
	}
	output.InstructionQuote = strings.TrimSpace(output.InstructionQuote)
	if *output.InstructionLeak && (output.InstructionQuote == "" || !strings.Contains(text, output.InstructionQuote)) {
		return r, errors.New("指导泄漏引用不属于当前正文")
	}
	review.Output = output
	r.Hits = remaining
	r.ContextReview = &review
	r.LatencyMS += review.LatencyMS
	if *output.InstructionLeak {
		r.Error = "内部指导文字泄漏：" + output.InstructionQuote
	}
	r.Passed = r.Error == "" && len(remaining) == 0
	return r, nil
}
