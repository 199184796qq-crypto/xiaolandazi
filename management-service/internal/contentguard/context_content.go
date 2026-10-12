package contentguard

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ContextContent exposes rule content only. Occurrence identity, offsets and
// associations remain in the program's report and never need to be echoed.
func (p *Policy) ContextContent(r Report) ([]map[string]any, error) {
	if r.SHA256 != p.SHA256 || !NeedsContextReview(r) {
		return nil, errors.New("候选不是本规则的待语境复核结果")
	}
	rules := map[string]Rule{}
	for _, rule := range p.Data.Rules {
		rules[rule.ID] = rule
	}
	items := make([]map[string]any, 0, len(r.Hits))
	for _, hit := range r.Hits {
		rule, ok := rules[hit.RuleID]
		if !ok || rule.ReviewMode != "semantic" {
			return nil, errors.New("待复核命中没有对应语境规则")
		}
		items = append(items, map[string]any{"命中原文": hit.Text, "规则": map[string]any{"类别": rule.Category, "范围": rule.Scope, "条件": rule.Condition, "说明": rule.Note, "所需依据": rule.RequiredBasis}})
	}
	return items, nil
}

// One request reviews one paragraph. The model provides one content verdict;
// the program binds it to every pending occurrence. No partial/indexed result.
func ResolveContextContent(r Report, text string, review ContextReview) (Report, error) {
	if !NeedsContextReview(r) {
		return r, errors.New("不能由模型改写硬禁或已完成的审核")
	}
	decision, reason, err := parseContextContent(review.Raw)
	if err != nil {
		return r, err
	}
	review.TextSHA = fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	review.LexicalHits = append([]Hit(nil), r.Hits...)
	noLeak := false // Supplied guidance is removed by the request-aware program.
	review.Output = ContextOutput{InstructionLeak: &noLeak}
	for i := range r.Hits {
		review.Output.Items = append(review.Output.Items, ContextItem{ID: fmt.Sprintf("h%d", i+1), Decision: decision, Reason: reason})
	}
	r.ContextReview = &review
	r.LatencyMS += review.LatencyMS
	if decision == "pass" {
		r.Hits = nil
	}
	r.Passed = r.Error == "" && len(r.Hits) == 0
	return r, nil
}

func parseContextContent(raw string) (string, string, error) {
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "```") {
		if start := strings.IndexByte(body, '\n'); start >= 0 && strings.HasSuffix(body, "```") {
			body = strings.TrimSpace(body[start+1 : len(body)-3])
		}
	}
	var decision, reason string
	if strings.HasPrefix(body, "{") {
		// Content-only JSON is tolerated, but internal IDs are never read.
		var value struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if json.Unmarshal([]byte(body), &value) != nil {
			return "", "", errors.New("语境复核未返回明确结论")
		}
		decision, reason = value.Decision, value.Reason
	} else {
		lines := strings.SplitN(body, "\n", 2)
		decision = lines[0]
		if len(lines) == 2 {
			reason = strings.TrimSpace(lines[1])
		} else {
			// A single line with the agreed verdict plus a reason is also valid.
			for _, sep := range []string{"：", ":", "。", "，", ","} {
				if parts := strings.SplitN(body, sep, 2); len(parts) == 2 {
					if normalizeContentDecision(parts[0]) != "" {
						decision, reason = parts[0], parts[1]
						break
					}
				}
			}
		}
	}
	decision = normalizeContentDecision(decision)
	reason = strings.TrimSpace(reason)
	if decision == "" || len(reason) > 6000 {
		return "", "", errors.New("语境复核未返回明确结论")
	}
	return decision, reason, nil
}

func normalizeContentDecision(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"结论：", "结论:", "判断：", "判断:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.Trim(strings.TrimSpace(value), "*。.!！ ")
	switch strings.ToLower(value) {
	case "放行", "通过", "pass", "allow":
		return "pass"
	case "拦截", "不通过", "block", "reject":
		return "block"
	case "缺依据", "需要依据", "evidence_required":
		return "evidence_required"
	}
	return ""
}
