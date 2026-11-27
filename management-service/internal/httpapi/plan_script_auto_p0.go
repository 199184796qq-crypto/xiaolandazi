package httpapi

import (
	"context"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
)

// autoScriptP0Hits uses the same P0 mother, base context exceptions and
// confirmed plan exceptions as the editor and live voice gate.
func autoScriptP0Hits(state *anchorcycle.State, policy *contentguard.Policy, sentence string) []contentguard.Annotation {
	report := policy.Scan(sentence, state.Config.ComplianceScope)
	hits := make([]contentguard.Annotation, 0, len(report.Hits))
	for _, hit := range report.Hits {
		hits = append(hits, contentguard.Annotation{Hit: hit})
	}
	return state.FilterP0Annotations(policy, sentence, hits)
}

// Only the offending sentence goes to the rewrite model. If no candidate
// clears P0, the paid draft remains available for manual correction; saving
// still runs the server-side P0 gate.
func repairAutoScriptP0Sentences(state *anchorcycle.State, policy *contentguard.Policy, body string, rewrite func(string, []contentguard.Annotation) string) (string, bool) {
	sentences := splitRewriteSentences(body)
	for i, sentence := range sentences {
		hits := autoScriptP0Hits(state, policy, sentence)
		if len(hits) == 0 {
			continue
		}
		candidate := strings.TrimSpace(rewrite(sentence, hits))
		if candidate != "" && len(autoScriptP0Hits(state, policy, candidate)) == 0 {
			sentences[i] = candidate
		}
	}
	result := strings.Join(sentences, "")
	return result, len(autoScriptP0Hits(state, policy, result)) > 0
}

func (s *Server) repairAutoScriptP0(ctx context.Context, tenant, planID int64, gateway *agentgateway.Gateway, body string) (string, bool) {
	p0, _, err := loadP0Mother()
	if err != nil {
		log.Printf("auto script P0 unavailable tenant=%d plan=%d stage=load", tenant, planID)
		return body, true
	}
	state, policy, err := s.planRiskAnnotationP0(ctx, tenant, planID, p0)
	if err != nil {
		log.Printf("auto script P0 unavailable tenant=%d plan=%d stage=allowances", tenant, planID)
		return body, true
	}
	return repairAutoScriptP0Sentences(state, policy, body, func(sentence string, hits []contentguard.Annotation) string {
		terms := make([]string, 0, len(hits))
		seen := map[string]bool{}
		for _, hit := range hits {
			if !seen[hit.Text] {
				terms = append(terms, hit.Text)
				seen[hit.Text] = true
			}
		}
		prompt := "只改写下面这一句直播话术，保留原句的意思、商品链接、价格、数字和事实；不要增加新信息，不要输出其他句子。P0命中词不能出现在改写中。只返回JSON：{\"sentence\":\"改写后的这一句\"}。\nP0命中词：" + strings.Join(terms, "、") + "\n原句：" + strings.TrimSpace(sentence)
		for attempt := 0; attempt < 2; attempt++ {
			resp, err := s.autoScriptComplete(ctx, tenant, planID, gateway, agentgateway.Request{
				Stage: "script_auto_p0_sentence_rewrite", Messages: autoScriptMessages("你只修改这一句中的P0问题，其他意思保持。只返回严格JSON。", prompt),
				ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 200, EnableThinking: false, Timeout: 40 * time.Second,
			})
			if err != nil {
				break
			}
			var parsed struct {
				Sentence string `json:"sentence"`
			}
			if autoScriptJSON(resp.Text, &parsed) == nil && strings.TrimSpace(parsed.Sentence) != "" && len(autoScriptP0Hits(state, policy, parsed.Sentence)) == 0 {
				return strings.TrimSpace(parsed.Sentence)
			}
		}
		return ""
	})
}
